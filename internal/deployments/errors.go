package deployments

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func internalError(message string, cause error) error {
	return apperr.New(apperr.Internal, message, cause)
}

func deploymentsContractRequired() error {
	return apperr.New(apperr.InvalidArgument, "Deployments API requires anthropic-version and anthropic-beta: "+managedAgentsBeta, nil)
}

func deploymentRunsContractRequired() error {
	return apperr.New(apperr.InvalidArgument, "Deployment Runs API requires anthropic-version and anthropic-beta: "+managedAgentsBeta, nil)
}

func deploymentRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func deploymentAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func deploymentPermissionDenied() error {
	return apperr.New(apperr.PermissionDenied, "Credential cannot access deployments", nil)
}

func deploymentRunPermissionDenied() error {
	return apperr.New(apperr.PermissionDenied, "Credential cannot access deployment runs", nil)
}

func environmentLoadError(err error, environmentID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return apperr.New(apperr.NotFound, "Environment not found: "+environmentID, err)
	}
	return internalError("Environment operation failed", fmt.Errorf("load environment %q: %w", environmentID, err))
}

func resourceBuildError(err error) error {
	var refErr resourceReferenceError
	if !errors.As(err, &refErr) {
		return invalidRequest(err)
	}
	if refErr.ResourceType == "file" && errors.Is(refErr.Err, db.ErrNotFound) {
		return apperr.New(apperr.NotFound, "File not found: "+refErr.ResourceID, err)
	}
	if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrNotFound) {
		return apperr.New(apperr.NotFound, "Memory store not found: "+refErr.ResourceID, err)
	}
	if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrInvalidState) {
		return apperr.New(apperr.InvalidArgument, "memory store must not be archived", err)
	}
	return internalError("Could not validate deployment resource", fmt.Errorf("validate %s resource %q: %w", refErr.ResourceType, refErr.ResourceID, refErr.Err))
}

func deploymentLoadError(err error, deploymentID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return apperr.New(apperr.NotFound, "Deployment not found: "+deploymentID, err)
	}
	if errors.Is(err, db.ErrInvalidState) {
		return apperr.New(apperr.InvalidArgument, "deployment state does not allow this operation", err)
	}
	return internalError("Deployment operation failed", fmt.Errorf("load or mutate deployment %q: %w", deploymentID, err))
}

func deploymentRunLoadError(err error, runID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return apperr.New(apperr.NotFound, "Deployment run not found: "+runID, err)
	}
	return internalError("Deployment run operation failed", fmt.Errorf("load deployment run %q: %w", runID, err))
}

func deploymentFileMountConflict(cause error) error {
	return apperr.New(apperr.Conflict, "File resource mount_path conflicts with the session filesystem", cause)
}
