package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) handlePlatformWebSessionStream(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionId"))
	if sessionID == "" {
		notFound(w, r)
		return
	}
	s.sessions.StreamEvents(w, r, sessionID)
}
