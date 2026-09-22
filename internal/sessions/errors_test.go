package sessions

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
)

func TestMapSessionLoadError(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "not found", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Session not found: session_test"},
		{name: "invalid state", cause: db.ErrInvalidState, kind: apperr.InvalidArgument, message: "session state does not allow this operation"},
		{name: "file path conflict", cause: db.ErrFilestorePathExists, kind: apperr.Conflict, message: "File resource mount_path conflicts with the session filesystem"},
		{name: "memory store limit", cause: &db.SessionMemoryStoreLimitError{Limit: sessionresource.MaxMemoryStores}, kind: apperr.InvalidArgument, message: "at most 8 memory stores are allowed"},
		{name: "memory store duplicate", cause: &db.SessionMemoryStoreDuplicateError{}, kind: apperr.InvalidArgument, message: "memory_store_id must be unique"},
		{name: "unexpected failure", cause: errors.New("database failed"), kind: apperr.Internal, message: "Session operation failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapSessionLoadError(test.cause, "session_test")
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

func TestMapMemoryAttachError(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		message string
	}{
		{name: "client identity", cause: sessionresource.ErrMemoryStoreClientIdentity, message: sessionresource.ErrMemoryStoreClientIdentity.Error()},
		{name: "access", cause: sessionresource.ErrMemoryStoreAccess, message: sessionresource.ErrMemoryStoreAccess.Error()},
		{name: "instructions", cause: sessionresource.ErrMemoryStoreInstructionsTooLong, message: sessionresource.ErrMemoryStoreInstructionsTooLong.Error()},
		{name: "limit", cause: sessionresource.ErrMemoryStoreLimit, message: sessionresource.ErrMemoryStoreLimit.Error()},
		{name: "duplicate", cause: sessionresource.ErrMemoryStoreDuplicate, message: sessionresource.ErrMemoryStoreDuplicate.Error()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapResourceBuildError(test.cause)
			appErr, ok := errors.AsType[*apperr.Error](mapped)
			if !ok {
				t.Fatalf("error type = %T, want *apperr.Error", mapped)
			}
			if appErr.Kind != apperr.InvalidArgument || appErr.PublicMessage != test.message {
				t.Fatalf("error = (%v, %q), want (%v, %q)", appErr.Kind, appErr.PublicMessage, apperr.InvalidArgument, test.message)
			}
		})
	}
}
