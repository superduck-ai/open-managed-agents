package db

import (
	"errors"
	"math"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestRequireCompleteSecretEnvelope(t *testing.T) {
	t.Run("failure nil envelope", func(t *testing.T) {
		if err := requireCompleteSecretEnvelope(nil); !errors.Is(err, ErrIncompleteSecretEnvelope) {
			t.Fatalf("error = %v, want ErrIncompleteSecretEnvelope", err)
		}
	})
	t.Run("failure missing ciphertext", func(t *testing.T) {
		err := requireCompleteSecretEnvelope(&secrets.Envelope{
			Nonce:         []byte("nonce"),
			WrappedDEK:    []byte("wrap"),
			FormatVersion: 1,
			KeyProvider:   "local",
			KeyVersion:    1,
		})
		if !errors.Is(err, ErrIncompleteSecretEnvelope) {
			t.Fatalf("error = %v, want ErrIncompleteSecretEnvelope", err)
		}
	})
	t.Run("failure format_version above int32", func(t *testing.T) {
		err := requireCompleteSecretEnvelope(&secrets.Envelope{
			Ciphertext:    []byte("cipher"),
			Nonce:         []byte("nonce"),
			WrappedDEK:    []byte("wrap"),
			FormatVersion: math.MaxInt32 + 1,
			KeyProvider:   "local",
			KeyVersion:    1,
		})
		if !errors.Is(err, ErrIncompleteSecretEnvelope) {
			t.Fatalf("error = %v, want ErrIncompleteSecretEnvelope", err)
		}
	})
	t.Run("success complete envelope", func(t *testing.T) {
		if err := requireCompleteSecretEnvelope(&secrets.Envelope{
			Ciphertext:    []byte("cipher"),
			Nonce:         []byte("nonce"),
			WrappedDEK:    []byte("wrap"),
			FormatVersion: 1,
			KeyProvider:   "local",
			KeyVersion:    1,
		}); err != nil {
			t.Fatalf("error = %v, want nil", err)
		}
	})
}

func TestVaultCredentialInsertParamsRequiresEnvelope(t *testing.T) {
	_, err := vaultCredentialInsertParams(VaultCredential{
		UUID:             "11111111-1111-4111-8111-111111111111",
		ExternalID:       "vcrd_test",
		OrganizationUUID: "11111111-1111-4111-8111-111111111111",
		WorkspaceUUID:    "11111111-1111-4111-8111-111111111111",
		VaultUUID:        "11111111-1111-4111-8111-111111111111",
		VaultExternalID:  "vault_test",
		AuthType:         "static_bearer",
		CredentialKey:    "key",
		Auth:             []byte(`{}`),
		Metadata:         []byte(`{}`),
		SecretPayload:    []byte(`{"token":"x"}`),
	})
	if !errors.Is(err, ErrIncompleteSecretEnvelope) {
		t.Fatalf("vaultCredentialInsertParams() error = %v, want ErrIncompleteSecretEnvelope", err)
	}
}
