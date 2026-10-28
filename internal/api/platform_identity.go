package api

import (
	"errors"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"log/slog"
	"net/http"
	"strings"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

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
	header := strings.TrimSpace(r.Header.Get("X-Organization-UUID"))
	query := strings.TrimSpace(r.URL.Query().Get("organization_uuid"))
	if header != "" && query != "" && header != query {
		return auth.Principal{}, platformOrganizationInvalid()
	}
	organizationUUID := platformOrganizationOverrideID(r)
	if organizationUUID == "" {
		organizationUUID = session.OrganizationUUID
	}
	if _, err := uuid.Parse(organizationUUID); err != nil {
		return auth.Principal{}, platformOrganizationInvalid()
	}
	if header != "" && session.VerifiedLogin() && s.platformAuth.VerifiedLoginEnabled() {
		principal, granted, accessErr := s.resolvePlatformAdministrator(r, session, organizationUUID)
		if accessErr != nil {
			return auth.Principal{}, accessErr
		}
		if granted {
			return s.resolvePlatformWorkspaceScope(r, principal)
		}
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
	principal.AccountUUID = session.UserUUID
	principal.AccountExternalID = session.UserExternalID
	principal.OrganizationAccessSource = auth.OrganizationAccessMember
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
			if !auth.ValidatePlatformCSRFToken(auth.ExtractPlatformSessionKey(r), r.Header.Get("X-CSRF-Token")) {
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

func (s *Server) resolvePlatformAdministrator(r *http.Request, session platformsession.Session, organizationUUID string) (auth.Principal, bool, *httpapi.Error) {
	_, err := s.platformAccess.Authorize(r.Context(), session.UserUUID)
	if errors.Is(err, platformadmin.ErrDenied) {
		return auth.Principal{}, false, nil
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "authorize platform organization request", "actor_account_uuid", session.UserUUID, "error_code", "authorization_unavailable")
		return auth.Principal{}, false, platformAuthorizationUnavailable()
	}
	organization, err := s.db.GetPlatformOrganization(r.Context(), organizationUUID)
	if errors.Is(err, db.ErrNotFound) {
		return auth.Principal{}, false, platformOrganizationNotFound()
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "resolve platform target organization", "actor_account_uuid", session.UserUUID, "error_code", "organization_lookup_unavailable")
		return auth.Principal{}, false, platformAuthorizationUnavailable()
	}
	principal := session.Principal()
	principal.AccountUUID = session.UserUUID
	principal.AccountExternalID = session.UserExternalID
	principal.UserUUID = ""
	principal.UserExternalID = ""
	principal.OrganizationUUID = organization.UUID
	principal.OrganizationAccessSource = auth.OrganizationAccessPlatformAdmin
	principal.WorkspaceAccess = auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: string(auth.OrganizationAccessPlatformAdmin)}
	return principal, true, nil
}

func (s *Server) servePlatformPrincipal(w http.ResponseWriter, r *http.Request, next http.Handler, principal auth.Principal) {
	if !principal.PlatformOrganizationAdmin(principal.OrganizationUUID) {
		next.ServeHTTP(w, r)
		return
	}
	response := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	next.ServeHTTP(response, r)
	status := response.Status()
	if status == 0 {
		status = http.StatusOK
	}
	level, outcome := slog.LevelInfo, "success"
	if status >= 500 {
		level, outcome = slog.LevelError, "failed"
	} else if status >= 400 {
		level, outcome = slog.LevelWarn, "denied"
	}
	s.logger.Log(r.Context(), level, "platform organization request", "request_id", httpapi.RequestID(r.Context()), "actor_account_uuid", principal.AccountUUID, "organization_uuid", principal.OrganizationUUID, "workspace_uuid", principal.WorkspaceUUID, "access_source", principal.OrganizationAccessSource, "action", r.Method, "resource_type", "console_request", "outcome", outcome, "status", status)
}
