package memory

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

var (
	errInvalidMemoryDepth    = errors.New("depth must be 0 or 1")
	errUnalignedMemoryPrefix = errors.New("path_prefix must end with /")
)

func invalidRequest(err error) error {
	return apperr.New(apperr.InvalidArgument, err.Error(), err)
}

func memoryRuntimeUnavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "Workspace memory runtime is unavailable", cause)
}
