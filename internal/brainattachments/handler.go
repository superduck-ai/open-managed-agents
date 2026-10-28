package brainattachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

const MaxObjectBytes int64 = 4 << 20

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var filePattern = regexp.MustCompile(`^file_[A-Za-z0-9_-]+$`)

type Scope struct {
	OrganizationUUID string
	WorkspaceUUID    string
	SessionUUID      string
}

type Database interface {
	GetBrainImageAttachment(context.Context, string, string, string) (db.BrainImageAttachment, error)
	PutBrainImageAttachment(context.Context, db.BrainImageAttachment, func() error) (bool, error)
	GetSessionAttachmentFile(context.Context, string, string, string) (db.FileRecord, error)
}

type Handler struct {
	maxObjectBytes int64
	database       Database
	store          storage.ObjectStore
	authorize      func(http.ResponseWriter, *http.Request) (Scope, bool)
	logger         *slog.Logger
	errors         *httpapi.ErrorAdapter
}

func NewHandler(database Database, store storage.ObjectStore, authorize func(http.ResponseWriter, *http.Request) (Scope, bool), maxObjectBytes int64, logger *slog.Logger) *Handler {
	if maxObjectBytes <= 0 {
		maxObjectBytes = MaxObjectBytes
	}
	return &Handler{maxObjectBytes: maxObjectBytes, database: database, store: store, authorize: authorize, logger: logging.LoggerOrDefault(logger), errors: httpapi.NewErrorAdapter(logger)}
}

func (h *Handler) RegisterRoutes(router chi.Router) {
	router.Put("/image-attachments/{sha256}", h.put)
	router.Get("/image-attachments/{sha256}", h.read)
	router.Head("/image-attachments/{sha256}", h.read)
	router.Get("/message-attachments/{file_id}/content", h.source)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.authorize(w, r)
	if !ok {
		return
	}
	digest := chi.URLParam(r, "sha256")
	if !digestPattern.MatchString(digest) {
		h.errors.Write(w, r, invalidImage("Invalid image attachment digest"))
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.maxObjectBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			h.errors.Write(w, r, imageTooLarge())
		} else {
			h.errors.Write(w, r, invalidImage("Incomplete image attachment"))
		}
		return
	}
	sum := sha256.Sum256(data)
	mime := http.DetectContentType(data)
	supported := mime == "image/png" || mime == "image/jpeg" || mime == "image/gif" || mime == "image/webp"
	if !supported || r.Header.Get("Content-Type") != mime || hex.EncodeToString(sum[:]) != digest || r.ContentLength >= 0 && r.ContentLength != int64(len(data)) {
		h.errors.Write(w, r, invalidImage("Image attachment digest, length or media type does not match"))
		return
	}
	if h.store == nil {
		h.errors.Write(w, r, attachmentFailure(errors.New("storage unavailable")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	input := db.BrainImageAttachment{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: scope.SessionUUID, SHA256: digest, MediaType: mime, SizeBytes: int64(len(data)), Bucket: h.store.Name(), ObjectKey: "brain-image-attachments/v1/" + scope.WorkspaceUUID + "/" + scope.SessionUUID + "/" + digest}
	created, err := h.database.PutBrainImageAttachment(ctx, input, func() error {
		_, uploadErr := h.store.Upload(ctx, input.ObjectKey, bytes.NewReader(data), storage.UploadOptions{Size: input.SizeBytes, ContentType: mime})
		return uploadErr
	})
	if err != nil {
		h.failure(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpapi.WriteJSON(w, status, struct {
		AttachmentID string `json:"attachment_id"`
		MediaType    string `json:"media_type"`
		Bytes        int64  `json:"bytes"`
	}{AttachmentID: "sha256:" + digest, MediaType: mime, Bytes: input.SizeBytes})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.authorize(w, r)
	if !ok {
		return
	}
	digest := chi.URLParam(r, "sha256")
	if !digestPattern.MatchString(digest) {
		h.errors.Write(w, r, invalidImage("Invalid image attachment digest"))
		return
	}
	image, err := h.database.GetBrainImageAttachment(r.Context(), scope.WorkspaceUUID, scope.SessionUUID, digest)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", image.MediaType)
		w.Header().Set("Content-Length", strconv.FormatInt(image.SizeBytes, 10))
		w.Header().Set("ETag", `"sha256:`+digest+`"`)
		return
	}
	if h.store == nil {
		h.failure(w, r, errors.New("storage unavailable"))
		return
	}
	object, err := h.store.Open(r.Context(), image.ObjectKey, nil)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, MaxObjectBytes+1))
	sum := sha256.Sum256(data)
	if err != nil || int64(len(data)) != image.SizeBytes || hex.EncodeToString(sum[:]) != digest || http.DetectContentType(data) != image.MediaType {
		h.failure(w, r, errors.New("image attachment integrity check failed"))
		return
	}
	w.Header().Set("Content-Type", image.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("ETag", `"sha256:`+digest+`"`)
	_, _ = w.Write(data)
}

func (h *Handler) source(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.authorize(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "file_id")
	if !filePattern.MatchString(id) {
		h.errors.Write(w, r, invalidImage("Invalid message attachment ID"))
		return
	}
	file, err := h.database.GetSessionAttachmentFile(r.Context(), scope.WorkspaceUUID, scope.SessionUUID, id)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	if h.store == nil || file.S3Bucket != h.store.Name() {
		h.failure(w, r, errors.New("attachment bucket unavailable"))
		return
	}
	object, err := h.store.Open(r.Context(), file.S3Key, nil)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	defer object.Body.Close()
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(file.SizeBytes, 10))
	if _, err := io.Copy(w, object.Body); err != nil {
		h.logger.WarnContext(r.Context(), "send message attachment", "error", err)
	}
}

func (h *Handler) failure(w http.ResponseWriter, r *http.Request, err error) {
	h.errors.Write(w, r, attachmentFailure(err))
}
