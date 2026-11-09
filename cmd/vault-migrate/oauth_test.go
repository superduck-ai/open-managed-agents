package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type oauthWrappingKey struct {
	secrets.KeyProvider
	calls, failAt int
}

func (k *oauthWrappingKey) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	k.calls++
	if k.calls == k.failAt {
		return secrets.WrappedKey{}, errors.New("private-target-error")
	}
	return k.KeyProvider.WrapDEK(ctx, dek)
}

func TestOAuthLocalVersionRotationAndAtomicFailure(t *testing.T) {
	for _, externalID := range []string{"poap_fixture", "poap_FridayBuiltinOAuthClient"} {
		t.Run(externalID, func(t *testing.T) { testOAuthLocalVersionRotation(t, externalID) })
	}
}

func testOAuthLocalVersionRotation(t *testing.T, externalID string) {
	t.Helper()
	oldKey := bytes.Repeat([]byte{1}, 32)
	source, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: oldKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := localkeys.New(localkeys.KeyMaterial{Version: 2, KEK: bytes.Repeat([]byte{2}, 32)}, []localkeys.KeyMaterial{{Version: 1, KEK: oldKey}})
	if err != nil {
		t.Fatal(err)
	}
	original := secrets.NewService(source)
	id, err := platformoauth.SealClientField(t.Context(), original, platformoauth.ProviderClientIDBinding(externalID), "client")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := platformoauth.SealClientField(t.Context(), original, platformoauth.ProviderSecretBinding(externalID), "secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(oauthMigrationDocument{ExternalID: externalID, Version: 3, ClientIDEnvelope: id, ClientSecretEnvelope: secret})
	if err != nil {
		t.Fatal(err)
	}
	row := db.SecretMigrationRecord{UUID: "provider", OrganizationUUID: "platform", WorkspaceUUID: "platform", Document: raw}
	key := &oauthWrappingKey{KeyProvider: target, failAt: 2}
	service := secrets.NewService(key)
	if _, count, err := migrateOAuthDocument(t.Context(), service, "platform_oauth_providers", row, "local", 1, false); err != nil || count != 2 || key.calls != 0 {
		t.Fatal("预检调用目标或未校验双信封")
	}
	next, count, err := migrateOAuthDocument(t.Context(), service, "platform_oauth_providers", row, "local", 1, true)
	if err == nil || !errors.Is(err, secrets.ErrMigrationTarget) || count != 0 || next != nil || bytes.Contains([]byte(err.Error()), []byte("private-target")) {
		t.Fatal("第二字段失败未原子拒绝或泄漏")
	}
	key.failAt = 0
	next, count, err = migrateOAuthDocument(t.Context(), service, "platform_oauth_providers", row, "local", 1, true)
	if err != nil || count != 2 {
		t.Fatal(err)
	}
	var document oauthMigrationDocument
	if err := json.Unmarshal(next, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 4 || document.ClientIDEnvelope.KeyVersion != 2 || document.ClientSecretEnvelope.KeyVersion != 2 || !bytes.Equal(id.Ciphertext, document.ClientIDEnvelope.Ciphertext) {
		t.Fatal("轮换破坏密文或版本")
	}
	calls := key.calls
	row.Document = next
	if _, count, err := migrateOAuthDocument(t.Context(), service, "platform_oauth_providers", row, "local", 1, true); err != nil || count != 0 || calls != key.calls {
		t.Fatal("同 provider 版本轮换不幂等")
	}
}

func TestOAuthMigrationRejectsMalformedClients(t *testing.T) {
	for _, raw := range []string{
		`{"external_id":"poap_custom"}`,
		`{"external_id":"poap_custom","client_id_envelope":{},"client_secret_envelope":{}}`,
		`{"external_id":"poap_custom","client_id_envelope":{}}`,
	} {
		if _, _, err := migrateOAuthDocument(t.Context(), nil, "platform_oauth_providers", db.SecretMigrationRecord{Document: json.RawMessage(raw)}, "local", 0, false); err == nil {
			t.Fatal("无效客户端未失败关闭")
		}
	}
}
