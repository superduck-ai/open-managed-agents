package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	platformPreviewCacheControl = "private, max-age=604800"
	platformThumbnailMaxEdge    = 400
	platformThumbnailMaxPixels  = 40_000_000
)

type platformOrganizationScope struct {
	id            int64
	routeUUID     string
	workspaceID   int64
	workspaceUUID string
}

type platformUploadB64Request struct {
	FileName string `json:"file_name"`
	FileB64  string `json:"file_b64"`
	FileKind string `json:"file_kind"`
}

type platformUploadB64Response struct {
	FileKind       string             `json:"file_kind"`
	FileUUID       string             `json:"file_uuid"`
	FileName       string             `json:"file_name"`
	CreatedAt      string             `json:"created_at"`
	UserUUID       *string            `json:"user_uuid"`
	SizeBytes      *int64             `json:"size_bytes"`
	ThumbnailURL   string             `json:"thumbnail_url"`
	PreviewURL     string             `json:"preview_url"`
	ThumbnailAsset *platformFileAsset `json:"thumbnail_asset,omitempty"`
	PreviewAsset   *platformFileAsset `json:"preview_asset,omitempty"`
	UUID           string             `json:"uuid"`
}

type platformFileAsset struct {
	URL          string `json:"url"`
	FileVariant  string `json:"file_variant"`
	PrimaryColor string `json:"primary_color"`
	ImageWidth   int    `json:"image_width"`
	ImageHeight  int    `json:"image_height"`
}

func (h *Handler) RegisterPlatformRoutes(r chi.Router) {
	r.Post("/upload_b64", h.uploadBase64)
	r.Get("/files/{file_uuid}/thumbnail", h.thumbnailRoute)
	r.Get("/files/{file_uuid}/preview", h.previewRoute)
}

func (h *Handler) uploadBase64(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing sessionKey cookie"))
		return
	}
	scope, apiErr := h.resolvePlatformOrganizationScope(r, principal)
	if apiErr != nil {
		httpapi.WriteError(w, r, apiErr)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.platformUploadBodyLimit())
	defer r.Body.Close()

	var payload platformUploadB64Request
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		status := http.StatusBadRequest
		message := "Expected JSON body with file_name and file_b64"
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
			message = "File exceeds maximum size"
		}
		httpapi.WriteError(w, r, httpapi.NewError(status, "invalid_request_error", message))
		return
	}

	filename := strings.TrimSpace(payload.FileName)
	if filename == "" {
		filename = "file"
	}
	if err := validateFilename(filename); err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}

	content, err := decodePlatformBase64(payload.FileB64)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Invalid file_b64"))
		return
	}
	if int64(len(content)) > h.cfg.Storage.MaxFileBytes {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "invalid_request_error", "File exceeds maximum size"))
		return
	}

	fileExternalID, err := ids.New("file_")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not generate file ID"))
		return
	}
	fileUUID := uuid.NewString()
	contentType := detectBase64ContentType(filename, content)
	imageWidth, imageHeight, primaryColor := imageAssetInfoFromBytes(content)
	thumbnail, hasThumbnail, thumbnailErr := buildPlatformThumbnail(contentType, content)
	if thumbnailErr != nil {
		log.Printf("generate platform thumbnail filename=%s content_type=%s: %v", filename, contentType, thumbnailErr)
	}
	objectKey := fmt.Sprintf("workspaces/%s/files/%s/%s", scope.workspaceUUID, fileUUID, sanitizeForKey(filename))

	if err := h.store.Put(r.Context(), objectKey, bytes.NewReader(content), int64(len(content)), contentType); err != nil {
		log.Printf("put platform upload object: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not store file"))
		return
	}

	sum := sha256.Sum256(content)
	record := db.FileRecord{
		UUID:              fileUUID,
		ExternalID:        fileExternalID,
		WorkspaceID:       scope.workspaceID,
		Filename:          filename,
		MimeType:          contentType,
		SizeBytes:         int64(len(content)),
		SHA256:            hex.EncodeToString(sum[:]),
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
		log.Printf("create platform file metadata: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not create file metadata"))
		return
	}

	thumbnailWidth, thumbnailHeight := imageWidth, imageHeight
	if hasThumbnail {
		thumbnailKey := platformThumbnailKey(record)
		if thumbnailKey != "" {
			if err := h.store.Put(r.Context(), thumbnailKey, bytes.NewReader(thumbnail.Content), int64(len(thumbnail.Content)), thumbnail.ContentType); err != nil {
				log.Printf("put platform thumbnail object file_uuid=%s key=%s: %v", record.UUID, thumbnailKey, err)
			} else {
				thumbnailWidth = thumbnail.Width
				thumbnailHeight = thumbnail.Height
			}
		}
	}

	fileKind := strings.TrimSpace(payload.FileKind)
	if fileKind == "" {
		fileKind = inferPlatformFileKind(contentType)
	}
	httpapi.WriteJSON(w, http.StatusOK, h.platformUploadResponse(record, scope.routeUUID, fileKind, imageWidth, imageHeight, thumbnailWidth, thumbnailHeight, primaryColor))
}

func (h *Handler) thumbnailRoute(w http.ResponseWriter, r *http.Request) {
	h.streamPlatformFileVariant(w, r, "thumbnail")
}

func (h *Handler) previewRoute(w http.ResponseWriter, r *http.Request) {
	h.streamPlatformFileVariant(w, r, "preview")
}

func (h *Handler) streamPlatformFileVariant(w http.ResponseWriter, r *http.Request, variant string) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing sessionKey cookie"))
		return
	}
	scope, apiErr := h.resolvePlatformOrganizationScope(r, principal)
	if apiErr != nil {
		httpapi.WriteError(w, r, apiErr)
		return
	}

	fileUUID := strings.TrimSpace(chi.URLParam(r, "file_uuid"))
	if _, err := uuid.Parse(fileUUID); err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+fileUUID))
		return
	}

	record, err := h.db.GetFileByUUIDInOrganization(r.Context(), scope.id, fileUUID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found: "+fileUUID))
			return
		}
		log.Printf("get platform file %s metadata: %v", variant, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve file"))
		return
	}
	objectKey := record.S3Key
	objectContentType := record.MimeType
	if variant == "thumbnail" {
		if thumbnailKey := platformThumbnailKey(record); thumbnailKey != "" {
			thumbnailObject, thumbnailErr := h.store.Get(r.Context(), thumbnailKey)
			if thumbnailErr == nil {
				objectKey = thumbnailKey
				objectContentType = record.MimeType
				object := thumbnailObject
				defer object.Body.Close()
				streamPlatformObject(w, record.UUID, objectKey, variant, object, objectContentType)
				return
			}
			log.Printf("get platform thumbnail object file_uuid=%s key=%s failed, falling back to original: %v", fileUUID, thumbnailKey, thumbnailErr)
		}
	}

	object, err := h.store.Get(r.Context(), objectKey)
	if err != nil {
		log.Printf("get platform file %s object: %v", variant, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not retrieve file"))
		return
	}
	defer object.Body.Close()
	streamPlatformObject(w, record.UUID, objectKey, variant, object, objectContentType)
}

func streamPlatformObject(w http.ResponseWriter, fileUUID string, objectKey string, variant string, object storage.Object, contentType string) {
	if object.ContentType != "" {
		contentType = object.ContentType
	}
	w.Header().Set("Cache-Control", platformPreviewCacheControl)
	w.Header().Set("Content-Type", contentType)
	if object.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	copied, copyErr := io.Copy(w, object.Body)
	if copyErr != nil {
		if object.Size >= 0 {
			log.Printf("stream platform file %s failed file_uuid=%s key=%s bytes_copied=%d expected_size=%d: %v", variant, fileUUID, objectKey, copied, object.Size, copyErr)
		} else {
			log.Printf("stream platform file %s failed file_uuid=%s key=%s bytes_copied=%d: %v", variant, fileUUID, objectKey, copied, copyErr)
		}
		return
	}
	if object.Size >= 0 && copied != object.Size {
		log.Printf("stream platform file %s size mismatch file_uuid=%s key=%s bytes_copied=%d expected_size=%d", variant, fileUUID, objectKey, copied, object.Size)
	}
}

func (h *Handler) resolvePlatformOrganizationScope(r *http.Request, principal auth.Principal) (platformOrganizationScope, *httpapi.Error) {
	orgID := strings.TrimSpace(chi.URLParam(r, "orgUuid"))
	if orgID == "" || orgID == "default" {
		orgID = principal.OrganizationUUID
		if orgID == "" {
			orgID = principal.OrganizationExternalID
		}
	}
	if orgID != principal.OrganizationUUID && orgID != principal.OrganizationExternalID {
		return platformOrganizationScope{}, httpapi.NewError(http.StatusForbidden, "permission_error", "Organization not found")
	}
	return platformOrganizationScope{
		id:            principal.OrganizationID,
		routeUUID:     orgID,
		workspaceID:   principal.WorkspaceID,
		workspaceUUID: principal.WorkspaceUUID,
	}, nil
}

func (h *Handler) platformUploadBodyLimit() int64 {
	limit := h.cfg.Storage.MaxFileBytes + h.cfg.Storage.MaxFileBytes/3 + 1024*1024
	if limit < 1024*1024 {
		return 1024 * 1024
	}
	return limit
}

func (h *Handler) platformUploadResponse(record db.FileRecord, workspaceUUID string, fileKind string, imageWidth int, imageHeight int, thumbnailWidth int, thumbnailHeight int, primaryColor string) platformUploadB64Response {
	thumbnailURL := platformFileVariantURL(workspaceUUID, record.UUID, "thumbnail")
	previewURL := platformFileVariantURL(workspaceUUID, record.UUID, "preview")
	var thumbnailAsset *platformFileAsset
	var previewAsset *platformFileAsset
	if strings.HasPrefix(record.MimeType, "image/") && imageWidth > 0 && imageHeight > 0 {
		if primaryColor == "" {
			primaryColor = "6c5bb9"
		}
		if thumbnailWidth <= 0 || thumbnailHeight <= 0 {
			thumbnailWidth = imageWidth
			thumbnailHeight = imageHeight
		}
		thumbnailAsset = &platformFileAsset{URL: thumbnailURL, FileVariant: "thumbnail", PrimaryColor: primaryColor, ImageWidth: thumbnailWidth, ImageHeight: thumbnailHeight}
		previewAsset = &platformFileAsset{URL: previewURL, FileVariant: "preview", PrimaryColor: primaryColor, ImageWidth: imageWidth, ImageHeight: imageHeight}
	}
	return platformUploadB64Response{
		FileKind:       fileKind,
		FileUUID:       record.UUID,
		FileName:       record.Filename,
		CreatedAt:      record.CreatedAt.UTC().Format(time.RFC3339Nano),
		UserUUID:       nil,
		SizeBytes:      nil,
		ThumbnailURL:   thumbnailURL,
		PreviewURL:     previewURL,
		ThumbnailAsset: thumbnailAsset,
		PreviewAsset:   previewAsset,
		UUID:           record.UUID,
	}
}

func platformFileVariantURL(workspaceUUID string, fileUUID string, variant string) string {
	return fmt.Sprintf("/api/%s/files/%s/%s", workspaceUUID, fileUUID, variant)
}

func decodePlatformBase64(raw string) ([]byte, error) {
	value := strings.TrimSpace(raw)
	if comma := strings.IndexByte(value, ','); comma >= 0 && strings.Contains(strings.ToLower(value[:comma]), "base64") {
		value = value[comma+1:]
	}
	value = strings.Join(strings.Fields(value), "")
	if value == "" {
		return nil, errors.New("empty base64")
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func detectBase64ContentType(filename string, content []byte) string {
	if len(content) > 0 {
		if detected := http.DetectContentType(content); detected != "" && detected != "application/octet-stream" {
			return detected
		}
	}
	if ext := filepath.Ext(filename); ext != "" {
		if value := mime.TypeByExtension(ext); value != "" {
			return value
		}
	}
	return "application/octet-stream"
}

func inferPlatformFileKind(contentType string) string {
	if strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	return "file"
}

type platformThumbnail struct {
	Content     []byte
	ContentType string
	Width       int
	Height      int
}

func buildPlatformThumbnail(contentType string, content []byte) (platformThumbnail, bool, error) {
	if contentType != "image/jpeg" && contentType != "image/png" {
		return platformThumbnail{}, false, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return platformThumbnail{}, false, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return platformThumbnail{}, false, errors.New("invalid image dimensions")
	}
	if int64(cfg.Width)*int64(cfg.Height) > platformThumbnailMaxPixels {
		return platformThumbnail{}, false, fmt.Errorf("image dimensions %dx%d exceed thumbnail limit", cfg.Width, cfg.Height)
	}

	thumbWidth, thumbHeight := fitThumbnailDimensions(cfg.Width, cfg.Height, platformThumbnailMaxEdge)
	if thumbWidth == cfg.Width && thumbHeight == cfg.Height {
		return platformThumbnail{Content: content, ContentType: contentType, Width: thumbWidth, Height: thumbHeight}, true, nil
	}

	img, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return platformThumbnail{}, false, err
	}
	dst := image.NewRGBA(image.Rect(0, 0, thumbWidth, thumbHeight))
	resizeNearest(dst, img)

	var out bytes.Buffer
	switch contentType {
	case "image/jpeg":
		if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
			return platformThumbnail{}, false, err
		}
	case "image/png":
		if err := png.Encode(&out, dst); err != nil {
			return platformThumbnail{}, false, err
		}
	default:
		return platformThumbnail{}, false, nil
	}
	return platformThumbnail{Content: out.Bytes(), ContentType: contentType, Width: thumbWidth, Height: thumbHeight}, true, nil
}

func fitThumbnailDimensions(width int, height int, maxEdge int) (int, int) {
	if width <= 0 || height <= 0 || maxEdge <= 0 {
		return 0, 0
	}
	if width <= maxEdge && height <= maxEdge {
		return width, height
	}
	if width >= height {
		scaledHeight := (height*maxEdge + width/2) / width
		if scaledHeight < 1 {
			scaledHeight = 1
		}
		return maxEdge, scaledHeight
	}
	scaledWidth := (width*maxEdge + height/2) / height
	if scaledWidth < 1 {
		scaledWidth = 1
	}
	return scaledWidth, maxEdge
}

func resizeNearest(dst *image.RGBA, src image.Image) {
	dstBounds := dst.Bounds()
	srcBounds := src.Bounds()
	dstWidth := dstBounds.Dx()
	dstHeight := dstBounds.Dy()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()
	for y := 0; y < dstHeight; y++ {
		sourceY := srcBounds.Min.Y + y*srcHeight/dstHeight
		for x := 0; x < dstWidth; x++ {
			sourceX := srcBounds.Min.X + x*srcWidth/dstWidth
			dst.Set(dstBounds.Min.X+x, dstBounds.Min.Y+y, src.At(sourceX, sourceY))
		}
	}
}

func platformThumbnailKey(record db.FileRecord) string {
	extension := platformThumbnailExtension(record.MimeType)
	if extension == "" {
		return ""
	}
	return path.Join(path.Dir(record.S3Key), "variants", "thumbnail"+extension)
}

func platformThumbnailExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	default:
		return ""
	}
}

func imageAssetInfoFromBytes(content []byte) (int, int, string) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return 0, 0, ""
	}
	primaryColor := ""
	if len(content) <= 32*1024*1024 {
		primaryColor = sampledPrimaryColor(content)
	}
	return cfg.Width, cfg.Height, primaryColor
}

func sampledPrimaryColor(content []byte) string {
	img, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return ""
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return ""
	}
	stepX := width / 32
	if stepX < 1 {
		stepX = 1
	}
	stepY := height / 32
	if stepY < 1 {
		stepY = 1
	}
	var red, green, blue, count uint64
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			red += uint64(r)
			green += uint64(g)
			blue += uint64(b)
			count++
		}
	}
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%02x%02x%02x", uint8((red/count)>>8), uint8((green/count)>>8), uint8((blue/count)>>8))
}
