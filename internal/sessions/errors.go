package sessions

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

func sessionsBetaRequired() error {
	return apperr.New(apperr.InvalidArgument, "Sessions API requires beta=true", nil)
}

func sessionRouteNotFound() error {
	return apperr.New(apperr.NotFound, "Not found", nil)
}

func sessionAuthenticationRequired() error {
	return apperr.New(apperr.Unauthenticated, "Missing API key", nil)
}

func environmentKeyCannotAccessSession() error {
	return apperr.New(apperr.PermissionDenied, "Environment key cannot access this session", nil)
}

func environmentKeyCannotManageSession() error {
	return apperr.New(apperr.PermissionDenied, "Environment key cannot manage this session", nil)
}

func environmentKeyCannotManageSessions() error {
	return apperr.New(apperr.PermissionDenied, "Environment key cannot manage sessions", nil)
}

func environmentNotFound(environmentID string, cause error) error {
	return apperr.New(apperr.NotFound, "Environment not found: "+environmentID, cause)
}

func memoryStoreNotFound(memoryStoreID string, cause error) error {
	return apperr.New(apperr.NotFound, "Memory store not found: "+memoryStoreID, cause)
}

func sessionNotFound(sessionID string, cause error) error {
	return apperr.New(apperr.NotFound, "Session not found: "+sessionID, cause)
}

func threadNotFound(threadID string, cause error) error {
	return apperr.New(apperr.NotFound, "Thread not found: "+threadID, cause)
}

func resourceNotFound(resourceID string, cause error) error {
	return apperr.New(apperr.NotFound, "Resource not found: "+resourceID, cause)
}

func mapResourceBuildError(err error) error {
	if mapped, ok := mapFileResourcePersistenceError(err); ok {
		return mapped
	}
	var refErr resourceReferenceError
	if !errors.As(err, &refErr) {
		return invalidRequest(err)
	}
	if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrNotFound) {
		return memoryStoreNotFound(refErr.ResourceID, err)
	}
	if refErr.ResourceType == "memory_store" && errors.Is(refErr.Err, db.ErrInvalidState) {
		return apperr.New(apperr.InvalidArgument, "memory store must not be archived", err)
	}
	return internalError(
		"Could not validate session resource",
		fmt.Errorf("validate %s reference %q: %w", refErr.ResourceType, refErr.ResourceID, refErr.Err),
	)
}

func mapSessionLoadError(err error, sessionID string) error {
	if mapped, ok := mapFileResourcePersistenceError(err); ok {
		return mapped
	}
	if errors.Is(err, db.ErrNotFound) {
		return sessionNotFound(sessionID, err)
	}
	if errors.Is(err, db.ErrInvalidState) {
		return apperr.New(apperr.InvalidArgument, "session state does not allow this operation", err)
	}
	return internalError("Session operation failed", fmt.Errorf("session %q operation: %w", sessionID, err))
}

func mapFileResourcePersistenceError(err error) (error, bool) {
	var limitErr *db.SessionFileResourceLimitError
	if errors.As(err, &limitErr) {
		return invalidRequest(limitErr), true
	}
	var mountConflictErr *db.SessionFileMountConflictError
	if errors.As(err, &mountConflictErr) {
		return apperr.New(apperr.InvalidArgument, "file resource mount_path conflicts with another Session file resource", err), true
	}
	if errors.Is(err, db.ErrFileReferenceNotFound) {
		return apperr.New(apperr.NotFound, "File referenced by the session resource was not found", err), true
	}
	if errors.Is(err, db.ErrFilestorePathExists) {
		return apperr.New(apperr.Conflict, "File resource mount_path conflicts with the session filesystem", err), true
	}
	return nil, false
}

func mapThreadLoadError(err error, threadID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return threadNotFound(threadID, err)
	}
	return internalError("Thread operation failed", fmt.Errorf("thread %q operation: %w", threadID, err))
}

func mapResourceLoadError(err error, resourceID string) error {
	if errors.Is(err, db.ErrNotFound) {
		return resourceNotFound(resourceID, err)
	}
	if errors.Is(err, db.ErrFileInUse) {
		return apperr.New(apperr.Conflict, "File resource is referenced by a Session event", err)
	}
	return internalError("Resource operation failed", fmt.Errorf("resource %q operation: %w", resourceID, err))
}

func streamingUnsupported() error {
	return internalError("Streaming is not supported", errors.New("response writer does not implement http.Flusher"))
}
