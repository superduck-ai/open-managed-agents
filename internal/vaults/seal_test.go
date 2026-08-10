package vaults

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSealCredentialSecretRejectsEmptyPayload(t *testing.T) {
	t.Parallel()

	const want = "vault credential secret payload is required to seal"

	t.Run("failure empty payload", func(t *testing.T) {
		err := SealCredentialSecret(context.Background(), nil, &db.VaultCredential{})
		if err == nil || err.Error() != want {
			t.Fatalf("SealCredentialSecret() error = %v, want %q", err, want)
		}
	})
	t.Run("failure json null payload", func(t *testing.T) {
		err := SealCredentialSecret(context.Background(), nil, &db.VaultCredential{
			SecretPayload: []byte("null"),
		})
		if err == nil || err.Error() != want {
			t.Fatalf("SealCredentialSecret() error = %v, want %q", err, want)
		}
	})
}
