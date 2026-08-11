package skills

import (
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMapSkillPackageError(t *testing.T) {
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "too large", cause: packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}, kind: apperr.RequestTooLarge, message: "Skill package exceeds maximum size"},
		{name: "invalid package", cause: packageError{Status: http.StatusBadRequest, Message: "Missing required multipart field: files[]"}, kind: apperr.InvalidArgument, message: "Missing required multipart field: files[]"},
		{name: "unexpected read failure", cause: errors.New("disk failed"), kind: apperr.Internal, message: "Could not read skill package"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapSkillPackageError(test.cause)
			assertSkillAppError(t, mapped, test.kind, test.message)
			if !errors.Is(mapped, test.cause) {
				t.Fatalf("errors.Is(%v, cause) = false", mapped)
			}
		})
	}
}

func TestMapResolveVersionError(t *testing.T) {
	tests := []struct {
		name    string
		version string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "latest skill missing", version: "latest", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Skill not found: skill_test"},
		{name: "explicit version missing", version: "1", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Skill version not found: 1"},
		{name: "unexpected failure", version: "1", cause: errors.New("database failed"), kind: apperr.Internal, message: "Could not retrieve skill version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapResolveVersionError("skill_test", test.version, test.cause)
			assertSkillAppError(t, mapped, test.kind, test.message)
			if !errors.Is(mapped, test.cause) {
				t.Fatalf("errors.Is(%v, cause) = false", mapped)
			}
		})
	}
}

func assertSkillAppError(t *testing.T, err error, kind apperr.Kind, message string) {
	t.Helper()
	appErr, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		t.Fatalf("error type = %T, want *apperr.Error", err)
	}
	if appErr.Kind != kind || appErr.PublicMessage != message {
		t.Fatalf("error = (%v, %q), want (%v, %q)", appErr.Kind, appErr.PublicMessage, kind, message)
	}
}
