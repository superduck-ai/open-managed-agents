package codesessions

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/brainattachments"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func (h *Handler) WithImageAttachments(store storage.ObjectStore) *Handler {
	h.imageAttachments = brainattachments.NewHandler(h.db, store, func(w http.ResponseWriter, r *http.Request) (brainattachments.Scope, bool) {
		id := chi.URLParam(r, "code_session_id")
		claims, ok := h.authorizeSessionIngressClaims(w, r, id)
		if !ok {
			return brainattachments.Scope{}, false
		}
		epoch, err := strconv.ParseInt(r.Header.Get("X-Worker-Epoch"), 10, 64)
		if err != nil || claims.WorkerEpoch <= 0 || epoch != claims.WorkerEpoch {
			h.errorAdapter.Write(w, r, workerIngressAuthorizationFailure(db.ErrWorkerEpochMismatch))
			return brainattachments.Scope{}, false
		}
		session, found, err := h.db.GetSession(r.Context(), claims.WorkspaceUUID, claims.PublicSessionID)
		if err != nil {
			h.errorAdapter.Write(w, r, workerIngressAuthorizationFailure(err))
			return brainattachments.Scope{}, false
		}
		if !found {
			h.errorAdapter.Write(w, r, codeSessionRouteNotFound())
			return brainattachments.Scope{}, false
		}
		return brainattachments.Scope{OrganizationUUID: claims.OrganizationUUID, WorkspaceUUID: claims.WorkspaceUUID, SessionUUID: session.UUID}, true
	}, h.cfg.BrainImageAttachments.MaxObjectBytes, h.logger.With("component", "brain_attachments"))
	return h
}
