package agents

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func agentsBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Agents API requires beta=true", nil)
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
