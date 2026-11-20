package codesessions

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// SessionBudgetHeader identifies a local budget gate before the model proxy forwards a request.
const SessionBudgetHeader = "X-Oma-Session-Budget"

func (h *Handler) handleSessionBudget(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.authorizeSessionIngressClaims(w, r, chi.URLParam(r, "code_session_id"))
	if !ok {
		return
	}
	session, found, err := h.db.GetSession(r.Context(), claims.WorkspaceUUID, claims.PublicSessionID)
	if err != nil {
		h.writeIngressLoadError(w, r, err)
		return
	}
	if !found || session.ArchivedAt != nil || session.Status == "terminated" {
		h.errorAdapter.Write(w, r, codeSessionRouteNotFound())
		return
	}
	state := "available"
	if session.BudgetReachedAt != nil {
		state = "reached"
	}
	w.Header().Set(SessionBudgetHeader, state)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
