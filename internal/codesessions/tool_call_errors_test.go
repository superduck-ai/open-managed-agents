package codesessions

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestToolCallErrorsPreserveTransportClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind apperr.Kind
	}{
		{toolCallOperationError(db.ErrToolCallConflict), apperr.Conflict},
		{toolCallInputError(&http.MaxBytesError{Limit: 8 << 20}), apperr.RequestTooLarge},
		{toolCallOperationError(db.ErrNotFound), apperr.NotFound},
		{toolCallOperationError(db.ErrToolCallSandboxRetired), apperr.Gone},
	} {
		var app *apperr.Error
		if !errors.As(tc.err, &app) || app.Kind != tc.kind {
			t.Fatalf("wrong application error: %v", tc.err)
		}
	}
}

func TestToolCallResultStatusRequiresMatchingReason(t *testing.T) {
	for _, tc := range []struct {
		status, reason string
		valid          bool
	}{
		{"stopped", "", false},
		{"interrupted", "", false},
		{"interrupted", "crash", false},
		{"succeeded", "deadline", false},
		{"interrupted", "stop_requested", true},
		{"interrupted", "deadline", true},
		{"interrupted", "executor_shutdown", true},
		{"failed", "", true},
		{"succeeded", "", true},
	} {
		if got := toolCallResultStatusValid(tc.status, tc.reason); got != tc.valid {
			t.Fatalf("status=%s reason=%s valid=%v", tc.status, tc.reason, got)
		}
	}
}

func TestToolCallResultPreflightRejectsStateBeforeBody(t *testing.T) {
	digest := "same"
	size := int64(42)
	for _, tc := range []struct {
		state, digest string
		size          int64
		accepted      bool
	}{
		{"succeeded", "different", size, false},
		{"succeeded", digest, size + 1, false},
		{"finalizing", "different", size, false},
		{"running", digest, size, true},
		{"dispatched", digest, size, true},
		{"cancel_requested", digest, size, true},
		{"finalizing", digest, size, true},
		{"succeeded", digest, size, true},
		{"lost", digest, size, true},
	} {
		call := db.SandboxToolCall{State: tc.state, ResultSHA256: &digest, ResultBytes: &size}
		if got := toolCallResultStateAccepts(call, tc.digest, tc.size); got != tc.accepted {
			t.Fatalf("state=%s accepted=%v", tc.state, got)
		}
	}
}
