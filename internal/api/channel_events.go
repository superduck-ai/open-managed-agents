package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/channels"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func (s *Server) subscribeChannelEvents(ctx context.Context, apiKey, sessionID string, options sessions.StreamOptions) (channels.SessionEventSubscription, error) {
	principal, found, apiErr := s.authenticateWorkspaceAPIKeyContext(ctx, apiKey)
	if apiErr != nil {
		return nil, apiErr
	}
	if !found {
		return nil, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid API key")
	}
	workspace, err := s.db.GetAdminWorkspace(ctx, principal.OrganizationUUID, principal.WorkspaceUUID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace not allowed")
	}
	if err != nil {
		return nil, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	if workspace.ArchivedAt != nil {
		return nil, httpapi.NewError(http.StatusForbidden, "permission_error", "Workspace not allowed")
	}
	return s.sessions.SubscribeEvents(auth.WithPrincipal(ctx, principal), sessionID, options)
}
