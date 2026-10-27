package brainattachments

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"net/http"
)

func invalidImage(message string) *httpapi.Error {
	return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", message)
}

func imageTooLarge() *httpapi.Error {
	return httpapi.NewError(http.StatusRequestEntityTooLarge, "request_too_large", "Image attachment exceeds the size limit")
}

func attachmentFailure(err error) *httpapi.Error {
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, storage.ErrNotFound) {
		return httpapi.NewError(http.StatusNotFound, "not_found_error", "Image attachment not found")
	}
	return httpapi.NewError(http.StatusServiceUnavailable, "api_error", "Image attachment storage is unavailable")
}
