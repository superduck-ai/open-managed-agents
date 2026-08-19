package batches

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBatchErrorsPreserveSpecialHTTPContracts(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		kind    apperr.Kind
		message string
	}{
		{name: "delete before ended", err: messageBatchMustBeEnded(db.ErrInvalidState), kind: apperr.InvalidState, message: "Message batch must be ended before deletion"},
		{name: "results before ended", err: messageBatchHasNotEnded(), kind: apperr.InvalidArgument, message: "Message batch has not ended"},
		{name: "results unavailable", err: messageBatchResultsUnavailable(), kind: apperr.NotFound, message: "Message batch results are not available"},
		{name: "upstream unavailable", err: batchServiceUnavailable(), kind: apperr.Unavailable, message: "anthropic_upstream.api_key is required for Message Batches"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			appErr, ok := errors.AsType[*apperr.Error](test.err)
			if !ok {
				t.Fatalf("error type = %T, want *apperr.Error", test.err)
			}
			if appErr.Kind != test.kind || appErr.PublicMessage != test.message {
				t.Fatalf("error = (%v, %q), want (%v, %q)", appErr.Kind, appErr.PublicMessage, test.kind, test.message)
			}
		})
	}
}
