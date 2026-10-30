package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/platformapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
)

func (s *Server) registerPlatformAccountRoutes(router chi.Router) {
	service := s.platformAccess
	platformapi.RegisterPlatformAccountRoutes(router, s.db, func(ctx context.Context, session platformsession.Session) (platformapi.PlatformAccess, error) {
		access := platformadmin.Permissions(false)
		if s.platformAuth.VerifiedLoginEnabled() && session.VerifiedLogin() {
			_, err := service.Authorize(ctx, session.UserUUID)
			if err != nil && !errors.Is(err, platformadmin.ErrDenied) {
				return platformapi.PlatformAccess{}, err
			}
			access = platformadmin.Permissions(err == nil)
		}
		return platformapi.PlatformAccess{Role: access.Role, Permissions: access.Permissions}, nil
	})
}

func (s *Server) registerPlatformAdminRoutes(router chi.Router) {
	service := platformadmin.New(s.db, s.logger.With("component", "platform_admin"))
	service.ConfigureDelivery(s.invitationJobs)
	loader := func(r *http.Request) (platformsession.Session, error) {
		session, err := s.loadPlatformSession(r)
		if err != nil {
			if err.Status == http.StatusUnauthorized {
				return session, platformadmin.ErrUnauthenticated
			}
			return session, platformadmin.ErrUnavailable
		}
		return session, nil
	}
	router.Mount("/api/platform-admin", platformadmin.NewHandler(service, loader, s.platformAuth.VerifiedLoginEnabled()).Routes())
}
