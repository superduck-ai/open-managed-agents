package tunnels

import (
	"log/slog"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func logManagementSuccess(logger *slog.Logger, r *http.Request, message, tunnelID string) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	logScopedManagementSuccess(logger, r, message, tunnelID, principal.OrganizationUUID, principal.WorkspaceUUID)
}

func logScopedManagementSuccess(
	logger *slog.Logger,
	r *http.Request,
	message string,
	tunnelID string,
	organizationUUID string,
	workspaceUUID string,
) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	actorID := principal.UserUUID
	if actorID == "" {
		actorID = principal.APIKeyUUID
	}
	logger.InfoContext(
		r.Context(), message,
		"request_id", httpapi.RequestID(r.Context()),
		"organization_id", organizationUUID,
		"workspace_id", workspaceUUID,
		"tunnel_id", tunnelID,
		"actor_type", principal.CredentialType,
		"actor_id", actorID,
	)
}

func (h *Handler) logManagementSuccess(r *http.Request, message, tunnelID string) {
	logManagementSuccess(h.logger, r, message, tunnelID)
}
