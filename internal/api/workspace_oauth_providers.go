package api

import (
	"cmp"
	"errors"
	"github.com/go-chi/chi/v5"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func (s *Server) resolveEnvOAuthWorkspace(w http.ResponseWriter, r *http.Request, principal auth.Principal, workspaceID string) (db.AdminWorkspace, bool) {
	if workspaceID == "" || workspaceID == "default" {
		workspaceID = principal.WorkspaceExternalID
	}
	workspace, access, err := s.workspaceAccess.ResolvePrincipal(r.Context(), principal, workspaceID)
	if err != nil || !access.UseResources() || workspace.ArchivedAt != nil {
		status := http.StatusForbidden
		if err != nil && !errors.Is(err, workspaceaccess.ErrDenied) {
			status = http.StatusInternalServerError
		}
		writeEnvOAuthStartError(w, status, envOAuthErrorInvalidRequest, "")
		return workspace, false
	}
	return workspace, true
}

func (s *Server) handleCreateWorkspaceOAuthProvider(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorizeEnvOAuthOrganization(w, r)
	if !ok {
		return
	}
	workspace, ok := s.resolveEnvOAuthWorkspace(w, r, principal, strings.TrimSpace(r.URL.Query().Get("workspace_id")))
	if !ok {
		return
	}
	input, err := httpapi.DecodeObjectBodyAs[platformadmin.OAuthProviderCreateInput](w, r, 1<<20)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return
	}
	input.WorkspaceUUID = workspace.UUID
	service := platformadmin.NewOAuthProviderService(s.db, s.vaultSecrets, s.logger).WithEnvironment(s.cfg.Env)
	provider, err := service.Create(r.Context(), platformadmin.Actor{AccountUUID: cmp.Or(principal.AccountUUID, principal.UserUUID)}, *input)
	s.writeWorkspaceOAuthProviderResult(w, r, provider, err, http.StatusCreated)
}

func (s *Server) handleWorkspaceOAuthProvider(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorizeEnvOAuthOrganization(w, r)
	if !ok {
		return
	}
	workspace, ok := s.resolveEnvOAuthWorkspace(w, r, principal, strings.TrimSpace(r.URL.Query().Get("workspace_id")))
	if !ok {
		return
	}
	service := platformadmin.NewOAuthProviderService(s.db, s.vaultSecrets, s.logger).WithEnvironment(s.cfg.Env)
	externalID := chi.URLParam(r, "providerID")
	if r.Method == http.MethodGet {
		provider, err := service.GetWorkspace(r.Context(), workspace.UUID, externalID)
		s.writeWorkspaceOAuthProviderResult(w, r, provider, err, http.StatusOK)
		return
	}
	input, err := httpapi.DecodeObjectBodyAs[platformadmin.OAuthProviderUpdateInput](w, r, 1<<20)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return
	}
	input.WorkspaceUUID = workspace.UUID
	provider, err := service.Update(r.Context(), platformadmin.Actor{AccountUUID: cmp.Or(principal.AccountUUID, principal.UserUUID)}, externalID, *input)
	s.writeWorkspaceOAuthProviderResult(w, r, provider, err, http.StatusOK)
}

func (s *Server) writeWorkspaceOAuthProviderResult(w http.ResponseWriter, r *http.Request, provider platformadmin.OAuthProviderView, err error, status int) {
	if err != nil {
		if public, ok := errors.AsType[*platformadmin.Error](err); ok {
			httpapi.WriteJSON(w, public.Status, map[string]any{"error": map[string]string{"code": public.Code, "message": public.Message}})
			return
		}
		s.logger.ErrorContext(r.Context(), "workspace oauth provider request failed", "error", err)
		writeEnvOAuthStartError(w, http.StatusServiceUnavailable, envOAuthErrorProviderUnavailable, "")
		return
	}
	httpapi.WriteJSON(w, status, provider)
}
