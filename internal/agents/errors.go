package agents

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
)

var errInvalidMCPServerName = errors.New("mcp_servers.name must contain 1 to 255 characters")
var errInvalidMCPToolsetServerName = errors.New("mcp_toolset.mcp_server_name must contain 1 to 255 characters")

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func agentMutationError(err error) error {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return err
	}
	return invalidRequest(err)
}

func configuredModelError(err error) error {
	if errors.Is(err, llmproviders.ErrNotConfigured) {
		return apperr.New(
			apperr.Unavailable,
			"This workspace has no LLM provider configured",
			err,
		)
	}
	return apperr.New(
		apperr.Internal,
		"Workspace model configuration is unavailable",
		err,
	)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func agentRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func agentAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func agentNotFound(agentID string, cause error) error {
	return apperr.New(apperr.NotFound, "Agent not found: "+agentID, cause)
}

func archivedAgentCannotBeUpdated(cause error) error {
	return apperr.New(apperr.InvalidArgument, "Archived agents cannot be updated", cause)
}

func agentVersionConflict(cause error) error {
	return apperr.New(apperr.Conflict, "Agent version does not match current version", cause)
}
