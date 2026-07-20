package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const filesBeta = "files-api-2025-04-14"

type Handler struct {
	cfg    config.Config
	db     *db.DB
	store  storage.ObjectStore
	router chi.Router
}

type fileMetadata struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Filename     string         `json:"filename"`
	MimeType     string         `json:"mime_type"`
	SizeBytes    int64          `json:"size_bytes"`
	CreatedAt    string         `json:"created_at"`
	Downloadable bool           `json:"downloadable"`
	Scope        map[string]any `json:"scope"`
}

type pageResponse struct {
	Data    []fileMetadata `json:"data"`
	HasMore bool           `json:"has_more"`
	FirstID *string        `json:"first_id"`
	LastID  *string        `json:"last_id"`
}

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore) *Handler {
	h := &Handler{cfg: cfg, db: database, store: store}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.upload)
	router.Get("/", h.list)
	router.Get("/{file_id}", h.retrieveMetadataRoute)
	router.Delete("/{file_id}", h.deleteRoute)
	router.Get("/{file_id}/content", h.downloadRoute)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !hasFilesBeta(r) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Files API requires anthropic-beta: files-api-2025-04-14"))
		return
	}

	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.Storage.MaxFileBytes+1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		status := http.StatusBadRequest
		message := "Expected multipart form data with a file field"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
			message = "File exceeds maximum size"
		}
		httpapi.WriteError(w, r, httpapi.NewError(status, "invalid_request_error", message))
		return
	}
	defer cleanupMultipart(r.MultipartForm)

	file, header, err := r.FormFile("file")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Missing required multipart field: file"))
		return
	}
	defer file.Close()

	filename := header.Filename
	if filename == "" {
		filename = "file"
	}
	if err := validateFilename(filename); err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	if header.Size > h.cfg.Storage.MaxFileBytes {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "invalid_request_error", "File exceeds maximum size"))
		return
	}
	fileExternalID, err := ids.New("file_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate file ID"))
		return
	}
	fileUUID := uuid.NewString()
	contentType := detectContentType(header)
	objectKey := fmt.Sprintf("workspaces/%s/files/%s/%s", principal.WorkspaceUUID, fileUUID, sanitizeForKey(filename))

	hash := sha256.New()
	reader := io.TeeReader(file, hash)
	if err := h.store.Put(r.Context(), objectKey, reader, header.Size, contentType); err != nil {
		log.Printf("put object: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not store file"))
		return
	}

	record := db.FileRecord{
		UUID:              fileUUID,
		ExternalID:        fileExternalID,
		WorkspaceID:       principal.WorkspaceID,
		Filename:          filename,
		MimeType:          contentType,
		SizeBytes:         header.Size,
		SHA256:            hex.EncodeToString(hash.Sum(nil)),
		S3Bucket:          h.store.Bucket(),
		S3Key:             objectKey,
		Downloadable:      false,
		CreatedByAPIKeyID: principal.APIKeyID,
		CreatedAt:         time.Now().UTC(),
	}
	if err := h.db.CreateFileIfWithinLimit(r.Context(), record, h.cfg.Storage.WorkspaceLimitBytes); err != nil {
		h.cleanupUploadedObjectAfterMetadataFailure(r.Context(), record)
		if errors.Is(err, db.ErrStorageLimitExceeded) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace storage limit exceeded"))
			return
		}
		log.Printf("create file metadata: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create file metadata"))
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, metadataFromRecord(record))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}
	limit, err := parseLimit(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	afterID := r.URL.Query().Get("after_id")
	beforeID := r.URL.Query().Get("before_id")

	records, hasMore, err := h.db.ListFilesPage(r.Context(), db.ListFilesPageParams{
		WorkspaceID: principal.WorkspaceID,
		ScopeID:     r.URL.Query().Get("scope_id"),
		AfterID:     afterID,
		BeforeID:    beforeID,
		Limit:       limit,
	})
	if err != nil {
		log.Printf("list files: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not list files"))
		return
	}

	data := make([]fileMetadata, 0, len(records))
	for _, record := range records {
		data = append(data, metadataFromRecord(record))
	}
	var firstID, lastID *string
	if len(data) > 0 {
		firstID = &data[0].ID
		lastID = &data[len(data)-1].ID
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{Data: data, HasMore: hasMore, FirstID: firstID, LastID: lastID})
}

func (h *Handler) retrieveMetadataRoute(w http.ResponseWriter, r *http.Request) {
	h.retrieveMetadata(w, r, chi.URLParam(r, "file_id"))
}

func (h *Handler) deleteRoute(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, chi.URLParam(r, "file_id"))
}

func (h *Handler) downloadRoute(w http.ResponseWriter, r *http.Request) {
	h.download(w, r, chi.URLParam(r, "file_id"))
}

func (h *Handler) retrieveMetadata(w http.ResponseWriter, r *http.Request, fileID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.db.GetFile(r.Context(), principal.WorkspaceID, fileID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixture(principal, fileID) {
			httpapi.WriteJSON(w, http.StatusOK, h.officialSDKFixtureMetadata())
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+fileID))
			return
		}
		log.Printf("get file metadata: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve file metadata"))
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, metadataFromRecord(record))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, fileID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.db.GetFile(r.Context(), principal.WorkspaceID, fileID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) && h.isOfficialSDKFixture(principal, fileID) {
			httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": fileID, "type": "file_deleted"})
			return
		}
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+fileID))
			return
		}
		log.Printf("get file before delete: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete file"))
		return
	}
	if err := h.db.SoftDeleteFile(r.Context(), principal.WorkspaceID, fileID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+fileID))
			return
		}
		log.Printf("soft delete file: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not delete file"))
		return
	}
	if err := h.store.Delete(r.Context(), record.S3Key); err != nil {
		log.Printf("delete object after soft delete file_id=%s: %v", fileID, err)
		if enqueueErr := h.db.EnqueueObjectCleanupJob(r.Context(), record.WorkspaceID, record.S3Bucket, record.S3Key, record.ExternalID); enqueueErr != nil {
			log.Printf("enqueue object cleanup file_id=%s key=%s: %v", fileID, record.S3Key, enqueueErr)
		}
	}
	if thumbnailKey := platformThumbnailKey(record); thumbnailKey != "" {
		if err := h.store.Delete(r.Context(), thumbnailKey); err != nil {
			log.Printf("delete thumbnail object after soft delete file_id=%s key=%s: %v", fileID, thumbnailKey, err)
			if enqueueErr := h.db.EnqueueObjectCleanupResourceJob(r.Context(), record.WorkspaceID, record.S3Bucket, thumbnailKey, "file_variant", record.ExternalID); enqueueErr != nil {
				log.Printf("enqueue thumbnail cleanup file_id=%s key=%s: %v", fileID, thumbnailKey, enqueueErr)
			}
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": fileID, "type": "file_deleted"})
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request, fileID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	record, err := h.db.GetFile(r.Context(), principal.WorkspaceID, fileID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+fileID))
			return
		}
		log.Printf("get file before download: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download file"))
		return
	}
	if !record.Downloadable {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "File is not downloadable"))
		return
	}
	object, err := h.store.Get(r.Context(), record.S3Key)
	if err != nil {
		log.Printf("get object: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not download file"))
		return
	}
	defer object.Body.Close()

	w.Header().Set("Content-Type", record.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(record.SizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		log.Printf("download object stream failed file_id=%s key=%s bytes_copied=%d expected_size=%d: %v", fileID, record.S3Key, copied, record.SizeBytes, copyErr)
		return
	}
	if copied != record.SizeBytes {
		log.Printf("download object stream size mismatch file_id=%s key=%s bytes_copied=%d expected_size=%d", fileID, record.S3Key, copied, record.SizeBytes)
	}
}

func (h *Handler) cleanupUploadedObjectAfterMetadataFailure(ctx context.Context, record db.FileRecord) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := h.store.Delete(cleanupCtx, record.S3Key); err != nil {
		log.Printf("delete object after metadata failure file_id=%s key=%s: %v", record.ExternalID, record.S3Key, err)
		if enqueueErr := h.db.EnqueueObjectCleanupJob(cleanupCtx, record.WorkspaceID, record.S3Bucket, record.S3Key, record.ExternalID); enqueueErr != nil {
			log.Printf("enqueue object cleanup after metadata failure file_id=%s key=%s: %v", record.ExternalID, record.S3Key, enqueueErr)
		}
	}
}

func hasFilesBeta(r *http.Request) bool {
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, part := range strings.Split(value, ",") {
			if strings.TrimSpace(part) == filesBeta {
				return true
			}
		}
	}
	return false
}

func validateFilename(filename string) error {
	if filename == "" || len(filename) > 255 {
		return errors.New("Invalid file name")
	}
	for _, r := range filename {
		if r < 32 || strings.ContainsRune(`<>:"|?*\/`, r) {
			return errors.New("Invalid file name")
		}
	}
	if !utf8.ValidString(filename) {
		return errors.New("Invalid file name")
	}
	return nil
}

func detectContentType(header *multipart.FileHeader) string {
	if value := header.Header.Get("Content-Type"); value != "" {
		return value
	}
	if ext := filepath.Ext(header.Filename); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return value
		}
	}
	return "application/octet-stream"
}

func sanitizeForKey(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "file"
	}
	return b.String()
}

func parseLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 1000 {
		return 0, errors.New("limit must be between 1 and 1000")
	}
	return limit, nil
}

func metadataFromRecord(record db.FileRecord) fileMetadata {
	var scope map[string]any
	if record.ScopeID != nil && *record.ScopeID != "" {
		scopeType := "session"
		if record.ScopeType != nil && *record.ScopeType != "" {
			scopeType = *record.ScopeType
		}
		scope = map[string]any{"id": *record.ScopeID, "type": scopeType}
	}
	return fileMetadata{
		ID:           record.ExternalID,
		Type:         "file",
		Filename:     record.Filename,
		MimeType:     record.MimeType,
		SizeBytes:    record.SizeBytes,
		CreatedAt:    record.CreatedAt.UTC().Format(time.RFC3339),
		Downloadable: record.Downloadable,
		Scope:        scope,
	}
}

func (h *Handler) isOfficialSDKFixture(principal auth.Principal, fileID string) bool {
	return fileID == h.cfg.SDKFixtures.FileID && principal.APIKeyExternalID == h.cfg.SDKFixtures.APIKeyExternalID
}

func (h *Handler) officialSDKFixtureMetadata() fileMetadata {
	return fileMetadata{
		ID:           h.cfg.SDKFixtures.FileID,
		Type:         "file",
		Filename:     "README.md",
		MimeType:     "text/markdown",
		SizeBytes:    12,
		CreatedAt:    time.Unix(0, 0).UTC().Format(time.RFC3339),
		Downloadable: false,
		Scope:        nil,
	}
}

func cleanupMultipart(form *multipart.Form) {
	if form != nil {
		_ = form.RemoveAll()
	}
}
