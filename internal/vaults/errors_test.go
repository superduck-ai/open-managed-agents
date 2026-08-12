package vaults

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestMapCreateCredentialError(t *testing.T) {
	unknown := errors.New("database unavailable")
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "duplicate", cause: db.ErrDuplicate, kind: apperr.Conflict, message: "Credential key already exists"},
		{name: "limit", cause: db.ErrLimitExceeded, kind: apperr.InvalidArgument, message: "Vault may contain at most 20 active credentials"},
		{name: "vault not found", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Vault not found: vlt_test"},
		{name: "unknown", cause: unknown, kind: apperr.Internal, message: "Could not create credential"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFault(t, mapCreateCredentialError(tt.cause, "vlt_test"), tt.kind, tt.message, tt.cause)
		})
	}
}

func TestMapUpdateCredentialError(t *testing.T) {
	unknown := errors.New("database unavailable")
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "not found", cause: db.ErrNotFound, kind: apperr.NotFound, message: "Credential not found: vcrd_test"},
		{name: "duplicate", cause: db.ErrDuplicate, kind: apperr.Conflict, message: "Credential key already exists"},
		{name: "version conflict", cause: db.ErrVersionConflict, kind: apperr.Conflict, message: "Credential was modified concurrently; reload and try again"},
		{name: "unknown", cause: unknown, kind: apperr.Internal, message: "Could not update credential"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFault(t, mapUpdateCredentialError(tt.cause, "vcrd_test"), tt.kind, tt.message, tt.cause)
		})
	}
}

func TestCredentialSecretError(t *testing.T) {
	openFailure := errors.New("ciphertext authentication failed")
	tests := []struct {
		name    string
		cause   error
		kind    apperr.Kind
		message string
	}{
		{name: "missing envelope", cause: ErrMissingSecretEnvelope, kind: apperr.InvalidArgument, message: ErrMissingSecretEnvelope.Error()},
		{name: "open failure", cause: openFailure, kind: apperr.Internal, message: "Could not update credential"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFault(t, credentialSecretError(tt.cause, "Could not update credential"), tt.kind, tt.message, tt.cause)
		})
	}
}

func TestStaticErrors(t *testing.T) {
	tests := []struct {
		name    string
		makeErr func() error
		kind    apperr.Kind
		message string
	}{
		{name: "beta required", makeErr: vaultBetaRequired, kind: apperr.InvalidArgument, message: "Vaults API requires beta=true"},
		{name: "route not found", makeErr: routeNotFound, kind: apperr.NotFound, message: "Not found"},
		{name: "vault archived", makeErr: vaultArchived, kind: apperr.InvalidArgument, message: "Vault is archived"},
		{name: "credential archived", makeErr: credentialArchived, kind: apperr.InvalidArgument, message: "Credential is archived"},
		{name: "credential requires MCP OAuth", makeErr: credentialRequiresMCPOAuth, kind: apperr.InvalidArgument, message: "mcp_oauth_validate requires an mcp_oauth credential"},
		{name: "missing API key", makeErr: missingAPIKey, kind: apperr.Unauthenticated, message: "Missing API key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFault(t, tt.makeErr(), tt.kind, tt.message, nil)
		})
	}
}

func assertFault(t *testing.T, err error, kind apperr.Kind, message string, cause error) {
	t.Helper()
	appErr, ok := errors.AsType[*apperr.Error](err)
	if !ok {
		t.Fatalf("error type = %T, want *apperr.Error", err)
	}
	if appErr.Kind != kind || appErr.PublicMessage != message {
		t.Fatalf(
			"application error = (%v, %q), want (%v, %q)",
			appErr.Kind,
			appErr.PublicMessage,
			kind,
			message,
		)
	}
	if cause == nil {
		if appErr.Unwrap() != nil {
			t.Fatalf("cause = %v, want nil", appErr.Unwrap())
		}
		return
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v) = false", cause)
	}
}
