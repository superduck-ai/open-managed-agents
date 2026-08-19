package sessions

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type sessionAccess string

const (
	sessionAccessRead            sessionAccess = "read"
	sessionAccessEventsRead      sessionAccess = "events_read"
	sessionAccessEventsSend      sessionAccess = "events_send"
	sessionAccessManageResources sessionAccess = "manage_resources"
)

func (h *Handler) authorizeSession(r *http.Request, sessionID string, access sessionAccess) (db.Session, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return db.Session{}, sessionAuthenticationRequired()
	}
	if h.isOfficialSDKFixturePrincipal(principal) && sessionID == h.cfg.SDKFixtures.SessionID {
		return h.fixtureDBSession(principal), nil
	}
	session, found, err := h.db.GetSession(r.Context(), principal.WorkspaceUUID, sessionID)
	if err != nil {
		return db.Session{}, mapSessionLoadError(err, sessionID)
	}
	if !found {
		return db.Session{}, mapSessionLoadError(db.ErrNotFound, sessionID)
	}
	if isSessionManagerCredential(principal) {
		return session, nil
	}
	if principal.CredentialType != auth.CredentialTypeEnvironmentKey {
		return db.Session{}, sessionAuthenticationRequired()
	}
	if session.EnvironmentExternalID != principal.EnvironmentExternalID {
		return db.Session{}, environmentKeyCannotAccessSession()
	}
	switch access {
	case sessionAccessRead, sessionAccessEventsRead, sessionAccessEventsSend:
		return session, nil
	default:
		return db.Session{}, environmentKeyCannotManageSession()
	}
}

func requireSessionManager(r *http.Request) (auth.Principal, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return auth.Principal{}, sessionAuthenticationRequired()
	}
	if !isSessionManagerCredential(principal) {
		return auth.Principal{}, environmentKeyCannotManageSessions()
	}
	return principal, nil
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
