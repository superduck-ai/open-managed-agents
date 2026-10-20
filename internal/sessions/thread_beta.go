package sessions

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

const managedThreadBeta = "managed-agents-2026-04-01"

func threadBetaEnabled(r *http.Request) bool {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if ok && principal.CredentialType == auth.CredentialTypePlatformSession && r.URL.Query().Get("beta") == "true" {
		return true
	}
	for _, value := range r.Header.Values("anthropic-beta") {
		for _, beta := range strings.Split(value, ",") {
			if strings.TrimSpace(beta) == managedThreadBeta {
				return true
			}
		}
	}
	return false
}

func (h *Handler) requireThreadBeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !threadBetaEnabled(r) {
			h.errorAdapter.Write(w, r, sessionRouteNotFound())
			return
		}
		next.ServeHTTP(w, r)
	})
}
