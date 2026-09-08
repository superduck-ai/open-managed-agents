package api

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

// loadPlatformSession 只验证登录身份，不要求原组织或工作区仍可访问。
func (s *Server) loadPlatformSession(r *http.Request) (platformsession.Session, *httpapi.Error) {
	sessionKey := auth.ExtractPlatformSessionKey(r)
	if sessionKey == "" {
		return platformsession.Session{}, platformIdentityUnauthorized()
	}
	session, err := s.platformStore.Get(r.Context(), sessionKey)
	if err != nil {
		if errors.Is(err, platformsession.ErrNotFound) {
			return platformsession.Session{}, platformIdentityUnauthorized()
		}
		s.logger.ErrorContext(r.Context(), "load platform login identity", "error", err)
		return platformsession.Session{}, platformIdentityUnavailable()
	}
	if session.VerifiedEmail != "" && session.UserUUID != "" {
		return session, nil
	}
	enriched, err := s.db.EnrichPlatformSession(r.Context(), session)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return platformsession.Session{}, platformIdentityUnauthorized()
		}
		s.logger.ErrorContext(r.Context(), "resolve platform login identity", "error", err)
		return platformsession.Session{}, platformIdentityUnavailable()
	}
	if err := s.platformStore.Save(r.Context(), sessionKey, enriched); err != nil {
		s.logger.ErrorContext(r.Context(), "save platform login identity", "error", err)
		return platformsession.Session{}, platformIdentityUnavailable()
	}
	return enriched, nil
}

func (s *Server) resolvePlatformSessionPrincipal(r *http.Request, session platformsession.Session) (auth.Principal, *httpapi.Error) {
	organizationUUID := platformOrganizationOverrideID(r)
	if organizationUUID == "" {
		organizationUUID = session.OrganizationUUID
	}
	if _, err := uuid.Parse(organizationUUID); err != nil {
		return auth.Principal{}, platformOrganizationDenied()
	}
	user, err := s.db.GetActivePlatformUserByEmail(r.Context(), organizationUUID, session.VerifiedEmail)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return auth.Principal{}, platformOrganizationDenied()
		}
		s.logger.ErrorContext(r.Context(), "resolve organization login membership", "error", err)
		return auth.Principal{}, platformIdentityUnavailable()
	}
	principal := session.Principal()
	principal.OrganizationUUID = user.OrganizationUUID
	principal.UserUUID = user.UUID
	principal.UserExternalID = user.ExternalID
	principal.WorkspaceUUID = ""
	principal.WorkspaceExternalID = ""
	return s.resolvePlatformWorkspaceScope(r, principal)
}

func (s *Server) platformIdentityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.loadPlatformSession(r)
		if err != nil {
			if err.Status == http.StatusUnauthorized {
				clearPlatformSessionCookies(w)
			}
			httpapi.WriteError(w, r, err)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			expected := platformsession.CSRFToken(auth.ExtractPlatformSessionKey(r))
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(expected)) != 1 {
				httpapi.WriteError(w, r, platformCSRFRejected())
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(platformsession.WithSession(r.Context(), session)))
	})
}

func verifiedInvitationEmail(r *http.Request) (string, bool) {
	session, ok := platformsession.SessionFromContext(r.Context())
	return session.VerifiedEmail, ok && session.VerifiedEmail != ""
}
