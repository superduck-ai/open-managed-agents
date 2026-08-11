package apperr_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestErrorPreservesCauseWithoutExposingPublicMessage(t *testing.T) {
	cause := errors.New("database detail")
	err := fmt.Errorf("operation failed: %w", apperr.New(
		apperr.Conflict,
		"Credential was modified concurrently",
		cause,
	))

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is did not reach the application error cause")
	}
	if strings.Contains(err.Error(), "Credential was modified concurrently") {
		t.Fatalf("Error() exposed PublicMessage: %q", err)
	}
}

func TestErrorSupportsAsType(t *testing.T) {
	want := apperr.New(apperr.NotFound, "Vault not found", nil)
	err := fmt.Errorf("retrieve vault: %w", want)

	got, ok := errors.AsType[*apperr.Error](err)
	if !ok || got != want {
		t.Fatalf("errors.AsType() = (%v, %v), want (%v, true)", got, ok, want)
	}
}
