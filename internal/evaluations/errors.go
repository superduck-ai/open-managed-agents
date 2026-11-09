package evaluations

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	invalidJSONMessage        = "Invalid JSON body"
	nameRequiredMessage       = "name is required"
	nameTooLongMessage        = "name must be at most 255 characters"
	descriptionTooLongMessage = "desc must be at most 1024 characters"
	queryRequiredMessage      = "query is required"
	queryTooLongMessage       = "query must be at most 16000 characters"
	answerTooLongMessage      = "expected answer must be at most 16000 characters"
	acceptanceTooLongMessage  = "acceptance must be at most 4000 characters"
	traceInvalidMessage       = "expected tool trace must be a JSON value"
	traceTooLongMessage       = "expected tool trace is too large"
	polarityInvalidMessage    = "polarity must be positive or negative"
	importRequiredMessage     = "import requires at least one case"
	importTooLargeMessage     = "import accepts at most 1000 cases"
	importInvalidMessage      = "import body must be a JSON array, JSONL, or CSV with a query column"
	caseNotFoundMessage       = "Evaluation case not found"
	datasetNotFoundMessage    = "Evaluation dataset not found"
	sessionRequiredMessage    = "session_id is required"
	turnsRequiredMessage      = "select at least one turn"
	turnInvalidMessage        = "turn is not in this session"
	generateCountMessage      = "count must be between 1 and 50"
	modelMissingMessage       = "No workspace model is configured"
	pageInvalidMessage        = "page is invalid"
	limitInvalidMessage       = "limit must be between 1 and 100"
	duplicateMessage          = "Evaluation resource already exists"
	archivedAgentMessage      = "agent must not be archived"
	createFailedMessage       = "Could not save evaluation"
	loadFailedMessage         = "Could not load evaluation"
	generateFailedMessage     = "Could not generate evaluation cases"
)

func invalidArgument(message string, cause error) error {
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func notFound(message string, cause error) error {
	return apperr.New(apperr.NotFound, message, cause)
}

func conflict(message string, cause error) error {
	return apperr.New(apperr.Conflict, message, cause)
}

func invalidState(message string, cause error) error {
	return apperr.New(apperr.InvalidState, message, cause)
}

func unavailable(message string, cause error) error {
	return apperr.New(apperr.Unavailable, message, cause)
}

func internalFailure(message string, cause error) error {
	return apperr.New(apperr.Internal, message, fmt.Errorf("%s: %w", message, cause))
}

func mapStoreError(err error, notFoundMessage string) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrNotFound):
		return notFound(notFoundMessage, err)
	case errors.Is(err, db.ErrDuplicate):
		return conflict(duplicateMessage, err)
	case errors.Is(err, db.ErrVersionConflict):
		return conflict("Agent version conflict", err)
	case errors.Is(err, db.ErrInvalidState):
		return invalidState(archivedAgentMessage, err)
	default:
		return internalFailure(createFailedMessage, err)
	}
}
