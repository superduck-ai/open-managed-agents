package agentruntime

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var (
	ErrUnsupportedProvider    = errors.New("agentruntime: unsupported provider")
	ErrProviderNotRegistered  = errors.New("agentruntime: provider factory not registered")
	ErrProfileInactive        = errors.New("agentruntime: runtime profile is inactive")
	ErrCapabilityUnsupported  = errors.New("agentruntime: capability unsupported")
	ErrInvalidCapabilityKey   = errors.New("agentruntime: unknown capability key")
	ErrResourceNotFound       = errors.New("agentruntime: resource not found")
	ErrCredentialsUnavailable = errors.New("agentruntime: credentials unavailable")
	ErrExternalProviderOnly   = errors.New("agentruntime: operation is only allowed for external providers")
)

func InvalidArgumentError(message string, cause error) error {
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func NotFoundError(message string, cause error) error {
	if cause == nil {
		cause = ErrResourceNotFound
	}
	return apperr.New(apperr.NotFound, message, cause)
}

func CapabilityUnsupportedError(capability string) error {
	return apperr.New(
		apperr.InvalidArgument,
		fmt.Sprintf("workspace runtime does not support capability %q", capability),
		fmt.Errorf("%w: %s", ErrCapabilityUnsupported, capability),
	)
}

func UnavailableError(message string, cause error) error {
	return apperr.New(apperr.Unavailable, message, cause)
}

func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNotFound) {
		return NotFoundError("resource not found", errors.Join(ErrResourceNotFound, err))
	}
	if errors.Is(err, db.ErrDuplicate) {
		return apperr.New(apperr.Conflict, "resource already exists", err)
	}
	return UnavailableError("runtime storage unavailable", err)
}
