package files

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
}

func (h *Handler) resolveExternalFileRuntime(ctx context.Context, principal auth.Principal) (agentruntime.Runtime, agentruntime.FileRuntime, agentruntime.Scope, error) {
	if h.runtimeResolver == nil {
		return nil, nil, agentruntime.Scope{}, fileRuntimeUnavailableError(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, nil, agentruntime.Scope{}, err
	}
	fileRT, ok := rt.(agentruntime.FileRuntime)
	if !ok {
		_ = rt.Close()
		return nil, nil, agentruntime.Scope{}, fileRuntimeUnavailableError(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	return rt, fileRT, scope, nil
}

func (h *Handler) writeExternalError(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.NewErrorAdapter(h.logger).Write(w, r, err)
}

func (h *Handler) uploadExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()

	constraints := rt.Capabilities().FileConstraints
	maxBytes := effectiveExternalMaxFileBytes(h.cfg.Storage.MaxFileBytes, constraints.MaxSizeBytes)
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024)
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
	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Could not read uploaded file"))
		return
	}
	if int64(len(content)) > maxBytes {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "invalid_request_error", "File exceeds maximum size"))
		return
	}
	contentType := detectContentType(header)
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		contentType = detectBase64ContentType(filename, content)
	}
	if !allowedExternalFileMIME(contentType, constraints) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "File type is not supported by this workspace runtime"))
		return
	}
	uploaded, err := fileRT.UploadFile(r.Context(), scope, agentruntime.UploadFileInput{
		Filename: filename,
		MimeType: contentType,
		Content:  content,
		ScopeID:  strings.TrimSpace(r.FormValue("scope_id")),
	})
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, metadataFromRuntimeFile(uploaded))
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	limit, err := parseLimit(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	offset, err := parseExternalFileOffset(r.URL.Query().Get("page"))
	if err != nil {
		httpapi.WriteError(w, r, invalidFilePageError())
		return
	}
	scopeID := strings.TrimSpace(r.URL.Query().Get("scope_id"))
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	page, err := fileRT.ListFiles(r.Context(), scope, agentruntime.ListFilesQuery{
		ScopeID: scopeID,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	data := make([]fileMetadata, 0, len(page.Items))
	for _, item := range page.Items {
		data = append(data, metadataFromRuntimeFile(item))
	}
	var firstID, lastID, nextPage *string
	if len(data) > 0 {
		firstID = &data[0].ID
		lastID = &data[len(data)-1].ID
	}
	if page.HasMore {
		cursor := page.NextCursor
		if cursor == "" {
			cursor = strconv.Itoa(offset + len(data))
		}
		nextPage = &cursor
	}
	httpapi.WriteJSON(w, http.StatusOK, pageResponse{
		Data:     data,
		HasMore:  page.HasMore,
		FirstID:  firstID,
		LastID:   lastID,
		NextPage: nextPage,
	})
}

func (h *Handler) retrieveMetadataExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, fileID string) {
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	record, err := fileRT.GetFile(r.Context(), scope, fileID)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, metadataFromRuntimeFile(record))
}

func (h *Handler) deleteExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, fileID string) {
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	if err := fileRT.DeleteFile(r.Context(), scope, fileID); err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"id": fileID, "type": "file_deleted"})
}

func (h *Handler) downloadExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, fileID string) {
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	stream, err := fileRT.DownloadFile(r.Context(), scope, fileID)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = stream.Close() }()
	h.writeExternalFileStream(r.Context(), w, fileID, stream, "")
}

func (h *Handler) consoleFileMetadataExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	record, err := fileRT.GetFile(r.Context(), scope, chi.URLParam(r, "fileId"))
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"filename": record.Filename})
}

func (h *Handler) consoleFileContentExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	fileID := chi.URLParam(r, "fileId")
	rt, fileRT, scope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	record, err := fileRT.GetFile(r.Context(), scope, fileID)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	if !previewableImage(record.MimeType) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "File is not previewable"))
		return
	}
	stream, err := fileRT.DownloadFile(r.Context(), scope, fileID)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = stream.Close() }()
	h.writeExternalFileStream(r.Context(), w, fileID, stream, "")
}

func (h *Handler) uploadBase64External(w http.ResponseWriter, r *http.Request, principal auth.Principal, scope platformOrganizationScope) {
	rt, fileRT, rtScope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()

	constraints := rt.Capabilities().FileConstraints
	maxBytes := effectiveExternalMaxFileBytes(h.cfg.Storage.MaxFileBytes, constraints.MaxSizeBytes)
	payload, err := httpapi.DecodeObjectBodyAs[platformUploadB64Request](w, r, h.platformUploadBodyLimit())
	defer r.Body.Close()
	if err != nil {
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
	if int64(len(content)) > maxBytes {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "invalid_request_error", "File exceeds maximum size"))
		return
	}
	contentType := detectBase64ContentType(filename, content)
	if !allowedExternalFileMIME(contentType, constraints) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "File type is not supported by this workspace runtime"))
		return
	}
	uploaded, err := fileRT.UploadFile(r.Context(), rtScope, agentruntime.UploadFileInput{
		Filename: filename,
		MimeType: contentType,
		Content:  content,
	})
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	imageWidth, imageHeight, primaryColor := imageAssetInfoFromBytes(content)
	fileKind := strings.TrimSpace(payload.FileKind)
	if fileKind == "" {
		fileKind = inferPlatformFileKind(contentType)
	}
	thumbnailURL := platformFileVariantURL(scope.routeUUID, uploaded.ID, "thumbnail")
	previewURL := platformFileVariantURL(scope.routeUUID, uploaded.ID, "preview")
	var thumbnailAsset, previewAsset *platformFileAsset
	if strings.HasPrefix(contentType, "image/") && imageWidth > 0 && imageHeight > 0 {
		if primaryColor == "" {
			primaryColor = "6c5bb9"
		}
		thumbnailAsset = &platformFileAsset{URL: thumbnailURL, FileVariant: "thumbnail", PrimaryColor: primaryColor, ImageWidth: imageWidth, ImageHeight: imageHeight}
		previewAsset = &platformFileAsset{URL: previewURL, FileVariant: "preview", PrimaryColor: primaryColor, ImageWidth: imageWidth, ImageHeight: imageHeight}
	}
	sizeBytes := uploaded.SizeBytes
	httpapi.WriteJSON(w, http.StatusOK, platformUploadB64Response{
		FileKind:       fileKind,
		FileUUID:       uploaded.ID,
		FileName:       uploaded.Filename,
		CreatedAt:      uploaded.CreatedAt.UTC().Format(time.RFC3339Nano),
		SizeBytes:      &sizeBytes,
		ThumbnailURL:   thumbnailURL,
		PreviewURL:     previewURL,
		ThumbnailAsset: thumbnailAsset,
		PreviewAsset:   previewAsset,
		UUID:           uploaded.ID,
	})
}

func (h *Handler) streamPlatformFileVariantExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	fileID := strings.TrimSpace(chi.URLParam(r, "file_uuid"))
	if fileID == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "File not found"))
		return
	}
	rt, fileRT, rtScope, err := h.resolveExternalFileRuntime(r.Context(), principal)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = rt.Close() }()
	stream, err := fileRT.DownloadFile(r.Context(), rtScope, fileID)
	if err != nil {
		h.writeExternalError(w, r, err)
		return
	}
	defer func() { _ = stream.Close() }()
	h.writeExternalFileStream(r.Context(), w, fileID, stream, platformPreviewCacheControl)
}

func (h *Handler) writeExternalFileStream(ctx context.Context, w http.ResponseWriter, fileID string, stream agentruntime.FileContentStream, cacheControl string) {
	contentType := stream.ContentType()
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}
	w.Header().Set("Content-Type", contentType)
	if size := stream.ContentLength(); size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, stream); err != nil {
		h.logger.ErrorContext(ctx, "stream external file failed", "file_id", fileID, "error", err)
	}
}

func effectiveExternalMaxFileBytes(configuredMax int64, runtimeMax int64) int64 {
	if runtimeMax > 0 && (configuredMax <= 0 || runtimeMax < configuredMax) {
		return runtimeMax
	}
	if configuredMax > 0 {
		return configuredMax
	}
	return 5 * 1024 * 1024
}

func allowedExternalFileMIME(contentType string, constraints agentruntime.FileUploadConstraints) bool {
	if constraints.AllowBinary || len(constraints.AllowedMIMEPrefixes) == 0 {
		return true
	}
	baseType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	for _, prefix := range constraints.AllowedMIMEPrefixes {
		if strings.HasPrefix(baseType, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

func parseExternalFileOffset(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errors.New("invalid page")
	}
	return offset, nil
}

func metadataFromRuntimeFile(file agentruntime.File) fileMetadata {
	var scope map[string]any
	if file.ScopeID != "" {
		scope = map[string]any{"id": file.ScopeID, "type": "session"}
	}
	return fileMetadata{
		ID:           file.ID,
		Type:         "file",
		Filename:     file.Filename,
		MimeType:     file.MimeType,
		SizeBytes:    file.SizeBytes,
		CreatedAt:    file.CreatedAt.UTC().Format(time.RFC3339),
		Downloadable: file.Downloadable,
		Scope:        scope,
	}
}
