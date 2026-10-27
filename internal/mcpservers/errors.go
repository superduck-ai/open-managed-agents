package mcpservers

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	invalidJSONMessage         = "Invalid JSON body"
	invalidLimitMessage        = "limit must be between 1 and 100"
	invalidPageMessage         = "page is invalid"
	nameRequiredMessage        = "name is required"
	nameTooLongMessage         = "name must be at most 255 characters"
	nameFormatMessage          = "name must use letters, numbers, underscores, hyphens, or periods and must not contain consecutive underscores"
	endpointTooLongMessage     = "url must be at most 2048 bytes"
	invalidEndpointMessage     = "url must be a valid HTTP or HTTPS MCP server URL without credentials or fragment"
	resourceNotFoundMessage    = "MCP server not found"
	duplicateResourceMessage   = "An MCP server with this name or URL already exists"
	internalServerErrorMessage = "Internal server error"
)

var errInvalidPageCursor = errors.New("invalid MCP server page cursor")

func invalidJSONRequest(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidJSONMessage, cause)
}

func invalidLimit(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidLimitMessage, cause)
}

func invalidPage(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidPageMessage, cause)
}

func nameRequired() error {
	return apperr.New(apperr.InvalidArgument, nameRequiredMessage, nil)
}

func nameTooLong() error {
	return apperr.New(apperr.InvalidArgument, nameTooLongMessage, nil)
}

func invalidNameFormat() error {
	return apperr.New(apperr.InvalidArgument, nameFormatMessage, nil)
}

func endpointTooLong() error {
	return apperr.New(apperr.InvalidArgument, endpointTooLongMessage, nil)
}

func invalidEndpoint(cause error) error {
	return apperr.New(apperr.InvalidArgument, invalidEndpointMessage, cause)
}

func resourceNotFound() error {
	return apperr.New(apperr.NotFound, resourceNotFoundMessage, nil)
}

func mapStoreError(err error, operation string) error {
	switch {
	case errors.Is(err, db.ErrDuplicate):
		return apperr.New(apperr.Conflict, duplicateResourceMessage, err)
	case errors.Is(err, db.ErrNotFound):
		return apperr.New(apperr.NotFound, resourceNotFoundMessage, err)
	default:
		return apperr.New(apperr.Internal, "Could not "+operation+" MCP server", fmt.Errorf("%s workspace MCP server: %w", operation, err))
	}
}
