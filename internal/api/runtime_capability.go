package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (s *Server) requireCapability(capability string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := auth.PrincipalFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if capErr := s.checkWorkspaceCapability(
				r.Context(),
				principal.OrganizationUUID,
				principal.WorkspaceUUID,
				principal.RuntimeProvider,
				capability,
			); capErr != nil {
				httpapi.WriteError(w, r, capErr)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) checkWorkspaceCapability(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	providerKind string,
	capability string,
) *httpapi.Error {
	caps := s.resolveWorkspaceCapabilities(ctx, organizationUUID, workspaceUUID, providerKind)
	supported, err := caps.Supports(capability)
	if err != nil {
		return httpapi.NewError(http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("Unknown workspace capability %q", capability))
	}
	if !supported {
		return httpapi.NewError(
			http.StatusBadRequest,
			"unsupported_capability_error",
			fmt.Sprintf("Workspace runtime does not support capability %q", capability),
		)
	}
	return nil
}

func (s *Server) resolveWorkspaceCapabilities(
	ctx context.Context,
	organizationUUID string,
	workspaceUUID string,
	providerKind string,
) agentruntime.CapabilitySet {
	if s.runtimeResolver != nil && organizationUUID != "" && workspaceUUID != "" {
		caps, _, err := s.runtimeResolver.WorkspaceCapabilities(ctx, organizationUUID, workspaceUUID)
		if err == nil {
			return caps
		}
	}
	kind := agentruntime.ProviderKind(providerKind)
	if kind == "" {
		kind = agentruntime.ProviderOMANative
	}
	return agentruntime.DefaultCapabilities(kind)
}
