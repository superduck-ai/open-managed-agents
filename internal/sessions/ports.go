package sessions

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

type sessionPortResponse struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Port      int    `json:"port"`
	URL       string `json:"url"`
}

func (h *Handler) WithSessionPorts(settings *sessionports.Settings) *Handler {
	h.ports = settings
	return h
}

func (h *Handler) retrievePortRoute(w http.ResponseWriter, r *http.Request) error {
	if _, err := requireSessionManager(r); err != nil {
		return err
	}
	if !h.ports.Enabled() {
		return sessionRouteNotFound()
	}
	sessionID := chi.URLParam(r, "session_id")
	port, err := strconv.Atoi(chi.URLParam(r, "port"))
	if err != nil || !h.ports.PortAllowed(port) {
		return invalidSessionPort(chi.URLParam(r, "port"))
	}
	session, err := h.authorizeSession(r, sessionID, sessionAccessRead)
	if err != nil {
		return err
	}
	if session.ArchivedAt != nil {
		return sessionNotFound(sessionID, nil)
	}
	address, err := h.ports.URL(session.UUID, port)
	if err != nil {
		return internalError("Could not build session port address", err)
	}
	httpapi.WriteJSON(w, http.StatusOK, sessionPortResponse{Type: "session_port", SessionID: session.ExternalID, Port: port, URL: address})
	return nil
}
