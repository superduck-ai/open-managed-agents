package messages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type messageFileContent struct {
	mediaType string
	data      []byte
}

type messageFileLoader func(string) (messageFileContent, *httpapi.Error)

func (h *Handler) WithFileStore(objects storage.ObjectStore) *Handler {
	h.fileObjects = objects
	return h
}

func (h *Handler) managedFileReferences(ctx context.Context, principal auth.Principal, body []byte) ([]byte, *httpapi.Error) {
	if principal.CodeSessionExternalID == "" || principal.PublicSessionExternalID == "" {
		return body, nil
	}
	cache := map[string]messageFileContent{}
	load := func(id string) (messageFileContent, *httpapi.Error) {
		if content, ok := cache[id]; ok {
			return content, nil
		}
		content, err := h.readMessageFile(ctx, principal, id)
		if err == nil {
			cache[id] = content
		}
		return content, err
	}
	return resolveMessageFileReferences(body, load)
}

func (h *Handler) readMessageFile(ctx context.Context, principal auth.Principal, id string) (messageFileContent, *httpapi.Error) {
	if h.database == nil || h.fileObjects == nil {
		return messageFileContent{}, fileReferenceUnavailableError()
	}
	record, err := h.database.GetFile(ctx, principal.WorkspaceUUID, id)
	if errors.Is(err, db.ErrNotFound) && principal.PublicSessionUUID != "" {
		record, err = h.database.GetSessionAttachmentFile(ctx, principal.WorkspaceUUID, principal.PublicSessionUUID, id)
	}
	if errors.Is(err, db.ErrNotFound) {
		return messageFileContent{}, fileReferenceNotFoundError()
	}
	if err != nil {
		return messageFileContent{}, fileReferenceUnavailableError()
	}

	if record.SizeBytes > llmproviders.MaxMessagesRequestBodyBytes {
		return messageFileContent{}, fileReferenceTooLargeError()
	}
	if record.S3Bucket != h.fileObjects.Name() {
		return messageFileContent{}, fileReferenceUnavailableError()
	}
	object, err := h.fileObjects.Open(ctx, record.S3Key, nil)
	if errors.Is(err, storage.ErrNotFound) {
		return messageFileContent{}, fileReferenceNotFoundError()
	}
	if err != nil || object.Body == nil {
		return messageFileContent{}, fileReferenceUnavailableError()
	}
	defer object.Body.Close()
	data, err := io.ReadAll(io.LimitReader(object.Body, llmproviders.MaxMessagesRequestBodyBytes+1))
	if err != nil {
		return messageFileContent{}, fileReferenceUnavailableError()
	}
	if int64(len(data)) > llmproviders.MaxMessagesRequestBodyBytes {
		return messageFileContent{}, fileReferenceTooLargeError()
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != record.SizeBytes || record.SHA256 != "" && hex.EncodeToString(digest[:]) != record.SHA256 {
		return messageFileContent{}, fileReferenceUnavailableError()
	}
	if record.MimeType == "text/plain" && !utf8.Valid(data) {
		return messageFileContent{}, fileReferenceInvalidTextError()
	}
	return messageFileContent{mediaType: record.MimeType, data: data}, nil
}
