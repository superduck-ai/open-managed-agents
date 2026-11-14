package channels

import (
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"net/http"
)

func rejectBrowserCSRF(r *http.Request, principal auth.Principal) error {
	if principal.CredentialType != auth.CredentialTypePlatformSession {
		return nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	default:
		sessionKey := auth.ExtractPlatformSessionKey(r)
		if sessionKey == "" || !auth.ValidatePlatformCSRFToken(sessionKey, r.Header.Get("X-CSRF-Token")) {
			return csrfRejected()
		}
		return nil
	}
}
