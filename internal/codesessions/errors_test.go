package codesessions

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMapCodeSessionLoadError(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "unexpected failure", cause: errors.New("database failed"), kind: apperr.Internal, message: "Could not load code session"},
		{name: "not found", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Code session not found"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapCodeSessionLoadError(test.cause, "code_session_test")
			appErr, ok := errors.AsType[*apperr.Error](mapped)
			if !ok {
				t.Fatalf("error type = %T, want *apperr.Error", mapped)
			}
			if appErr.Kind != test.kind || appErr.PublicMessage != test.message {
				t.Fatalf("error = (%v, %q), want (%v, %q)", appErr.Kind, appErr.PublicMessage, test.kind, test.message)
			}
			if !errors.Is(mapped, test.cause) {
				t.Fatalf("errors.Is(%v, cause) = false", mapped)
			}
		})
	}
}
