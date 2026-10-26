package codesessions

import (
	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"net/http"
)

type subagentThreadState struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Archived bool   `json:"archived"`
}

func scopedSubagentThreadState(record db.CodeSession, thread db.SessionThread) (subagentThreadState, error) {
	if thread.OrganizationUUID != record.OrganizationUUID || thread.WorkspaceUUID != record.WorkspaceUUID || thread.SessionExternalID != record.SessionExternalID || thread.ParentThreadExternalID == nil || thread.DeletedAt != nil {
		return subagentThreadState{}, db.ErrNotFound
	}
	return subagentThreadState{ID: thread.ExternalID, Status: thread.Status, Archived: thread.ArchivedAt != nil}, nil
}

func (h *Handler) handleGetCodeSessionSubagentThread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "code_session_id")
	if _, ok := h.sessionIngressAccessClaims(w, r, id, false); !ok {
		return
	}
	if _, _, ok := h.validateOptionalWorkerEpochRequest(w, r, id); !ok {
		return
	}
	record, err := h.requireCodeSession(r.Context(), id)
	if err != nil {
		h.writeIngressLoadError(w, r, err)
		return
	}
	thread, err := h.db.GetSessionThread(r.Context(), record.WorkspaceUUID, record.SessionExternalID, chi.URLParam(r, "thread_id"))
	if err != nil {
		h.writeIngressLoadError(w, r, err)
		return
	}
	state, err := scopedSubagentThreadState(record, thread)
	if err != nil {
		h.writeIngressLoadError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, http.StatusOK, state)
}
