package secrets_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type migrationProvider struct {
	secrets.KeyProvider
	name         string
	failWrap     bool
	failUnwrap   bool
	materialized [][]byte
}

func (p *migrationProvider) Name() string { return p.name }
func (p *migrationProvider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	if p.failWrap {
		return secrets.WrappedKey{}, errors.New("wrap unavailable")
	}
	return p.KeyProvider.WrapDEK(ctx, dek)
}
func (p *migrationProvider) UnwrapDEK(ctx context.Context, key secrets.WrappedKey) ([]byte, error) {
	if p.failUnwrap {
		return nil, errors.New("unwrap unavailable")
	}
	dek, err := p.KeyProvider.UnwrapDEK(ctx, key)
	p.materialized = append(p.materialized, dek)
	return dek, err
}

func TestMigration(t *testing.T) {
	for _, failure := range []string{"source", "target", "verification", "tamper", "none"} {
		t.Run(failure, func(t *testing.T) {
			oldKey, _ := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{1}, 32)}, nil)
			newKey, _ := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{2}, 32)}, nil)
			old := &migrationProvider{KeyProvider: oldKey, name: "local"}
			next := &migrationProvider{KeyProvider: newKey, name: "hashicorp_vault"}
			binding := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "vault", CredentialExternalID: "cred"}
			envelope := mustSeal(t, secrets.NewService(old), binding, []byte("secret"))
			old.failUnwrap = failure == "source"
			next.failWrap = failure == "target"
			next.failUnwrap = failure == "verification"
			if failure == "tamper" {
				envelope.WrappedDEK[0] ^= 1
			}
			service := secrets.NewService(next, old)
			checkErr := service.CheckMigration(t.Context(), envelope)
			if (checkErr != nil) != (failure == "source" || failure == "tamper") {
				t.Fatalf("source check: %v", checkErr)
			}
			result, err := service.MigrateEnvelope(t.Context(), envelope)
			if errors.Is(err, secrets.ErrMigrationTarget) != (failure == "target" || failure == "verification") {
				t.Fatalf("incorrect failure stage: %v", err)
			}
			for _, provider := range []*migrationProvider{old, next} {
				for _, dek := range provider.materialized {
					if !bytes.Equal(dek, make([]byte, len(dek))) {
						t.Fatal("temporary DEK was not cleared")
					}
				}
			}
			if failure != "none" {
				if err == nil || result.WrappedDEK != nil {
					t.Fatal("failed migration returned an envelope")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.KeyProvider != next.Name() || !bytes.Equal(result.Ciphertext, envelope.Ciphertext) || !bytes.Equal(result.Nonce, envelope.Nonce) {
				t.Fatal("migration changed payload or failed to change provider")
			}
			plain, err := secrets.NewService(next).Open(t.Context(), binding, result)
			defer clear(plain)
			if err != nil || string(plain) != "secret" {
				t.Fatalf("new provider cannot decrypt: %v", err)
			}
		})
	}
}
