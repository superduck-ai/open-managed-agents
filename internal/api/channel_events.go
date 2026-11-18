package api

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/channels"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func (s *Server) subscribeChannelEvents(ctx context.Context, apiKey, sessionID string, options sessions.StreamOptions) (channels.SessionEventSubscription, error) {
	ctx, err := s.channelContext(ctx, apiKey)
	if err != nil {
		return nil, err
	}
	return s.sessions.SubscribeEvents(ctx, sessionID, options)
}

func (s *Server) putChannelAttachment(ctx context.Context, apiKey, sessionID, path, mediaType string, body io.Reader) (db.FileRecord, error) {
	ctx, err := s.channelContext(ctx, apiKey)
	if err != nil {
		return db.FileRecord{}, err
	}
	return s.sessions.PutAttachment(ctx, sessionID, path, mediaType, body)
}

func (s *Server) channelContext(ctx context.Context, apiKey string) (context.Context, error) {
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
	return auth.WithPrincipal(ctx, principal), nil
}
