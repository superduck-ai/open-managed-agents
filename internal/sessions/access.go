package sessions

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

type sessionAccess string

const (
	sessionAccessRead            sessionAccess = "read"
	sessionAccessEventsRead      sessionAccess = "events_read"
	sessionAccessEventsSend      sessionAccess = "events_send"
	sessionAccessManageResources sessionAccess = "manage_resources"
)

func (h *Handler) authorizeSession(w http.ResponseWriter, r *http.Request, sessionID string, access sessionAccess) (db.Session, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return db.Session{}, false
	}
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		return h.fixtureDBSession(principal), true
	}
	session, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		h.writeSessionLoadError(w, r, err, sessionID)
		return db.Session{}, false
	}
	if isSessionManagerCredential(principal) {
		return session, true
	}
	if principal.CredentialType != auth.CredentialTypeEnvironmentKey {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return db.Session{}, false
	}
	if session.EnvironmentExternalID != principal.EnvironmentExternalID {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Environment key cannot access this session"))
		return db.Session{}, false
	}
	switch access {
	case sessionAccessRead, sessionAccessEventsRead, sessionAccessEventsSend:
		return session, true
	default:
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Environment key cannot manage this session"))
		return db.Session{}, false
	}
}

func requireSessionManager(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return auth.Principal{}, false
	}
	if !isSessionManagerCredential(principal) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "Environment key cannot manage sessions"))
		return auth.Principal{}, false
	}
	return principal, true
}

func isSessionManagerCredential(principal auth.Principal) bool {
	return principal.CredentialType == auth.CredentialTypeAPIKey ||
		principal.CredentialType == auth.CredentialTypePlatformSession
}

func workspaceUUIDFromRequest(r *http.Request) string {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.WorkspaceUUID
}

func organizationUUIDFromRequest(r *http.Request) string {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.OrganizationUUID
}

func workspaceExternalIDFromRequest(r *http.Request) string {
	principal, _ := auth.PrincipalFromContext(r.Context())
	return principal.WorkspaceExternalID
}
