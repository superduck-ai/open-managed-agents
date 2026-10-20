package codesessions

import (
	"context"
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const runtimeThreadHeader = "X-OMA-Session-Thread-ID"

func (h *Handler) runtimeSessionToolContext(ctx context.Context, record db.CodeSession, threadID string) (db.CodeSession, error) {
	if threadID == "" {
		return h.currentSessionToolContext(ctx, record)
	}
	snapshot, err := h.loadMCPThreadSnapshot(ctx, record, threadID)
	if err != nil {
		return db.CodeSession{}, err
	}
	return codeSessionWithToolSnapshot(record, snapshot)
}

func (h *Handler) mcpThreadSnapshot(ctx context.Context, identity upstreamProxyIdentity) (json.RawMessage, error) {
	record, found, err := h.db.GetCodeSession(ctx, identity.codeSessionExternalID)
	if err != nil {
		return nil, err
	}
	if !found || record.OrganizationUUID != identity.organizationUUID || record.WorkspaceUUID != identity.workspaceUUID {
		return nil, db.ErrNotFound
	}
	return h.loadMCPThreadSnapshot(ctx, record, identity.sessionThreadID)
}

func (h *Handler) loadMCPThreadSnapshot(ctx context.Context, record db.CodeSession, threadID string) (json.RawMessage, error) {
	if record.SessionExternalID == "" {
		return nil, db.ErrNotFound
	}
	thread, err := h.db.GetSessionThread(ctx, record.WorkspaceUUID, record.SessionExternalID, threadID)
	if err != nil {
		return nil, err
	}
	return validatedMCPThreadSnapshot(record, thread)
}

func validatedMCPThreadSnapshot(record db.CodeSession, thread db.SessionThread) (json.RawMessage, error) {
	if thread.OrganizationUUID != record.OrganizationUUID || thread.WorkspaceUUID != record.WorkspaceUUID ||
		thread.SessionExternalID != record.SessionExternalID || thread.ParentThreadExternalID == nil ||
		thread.Status == "terminated" || thread.ArchivedAt != nil || thread.DeletedAt != nil {
		return nil, db.ErrNotFound
	}
	if _, err := decodeRawJSONObject(thread.AgentSnapshot); err != nil {
		return nil, err
	}
	return thread.AgentSnapshot, nil
}
