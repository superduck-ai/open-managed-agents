package sessions

import (
	"context"
	"errors"
	"io"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

func (h *Handler) WithSessionAttachments(store codesessions.SessionAttachmentStore) *Handler {
	h.attachments = store
	return h
}

func (h *Handler) PutAttachment(ctx context.Context, sessionID, attachmentPath, mediaType string, body io.Reader) (db.FileRecord, error) {
	session, err := h.authorizeSessionContext(ctx, sessionID, sessionAccessManageResources)
	if err != nil {
		return db.FileRecord{}, err
	}
	if session.ArchivedAt != nil {
		return db.FileRecord{}, mapSessionLoadError(db.ErrInvalidState, sessionID)
	}
	environment, err := h.db.GetEnvironment(ctx, session.WorkspaceUUID, session.EnvironmentExternalID)
	if err != nil {
		return db.FileRecord{}, resourceEnvironmentLoadError(session.EnvironmentExternalID, err)
	}
	if err := validateEnvironmentSessionResources(environment, []normalizedSessionResource{{resource: db.SessionResource{ResourceType: db.SessionResourceTypeFile}}}); err != nil {
		return db.FileRecord{}, err
	}
	stored, err := h.db.GetSessionAttachmentFileByPath(ctx, session.WorkspaceUUID, session.UUID, attachmentPath)
	if errors.Is(err, db.ErrNotFound) {
		err = h.attachments.PutSessionAttachment(ctx, filestore.SessionAttachmentTarget{
			WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, Path: attachmentPath,
		}, mediaType, body)
		if err != nil {
			return db.FileRecord{}, err
		}
		stored, err = h.db.GetSessionAttachmentFileByPath(ctx, session.WorkspaceUUID, session.UUID, attachmentPath)
	}
	return stored, err
}
