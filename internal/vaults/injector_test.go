package vaults

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestOpenStaticBearerTokenMissingEnvelope(t *testing.T) {
	svc := newTestSecretsService(t)
	injector := &Injector{secretSvc: svc}
	if _, err := injector.openStaticBearerToken(context.Background(), &db.VaultCredential{ExternalID: "cred_missing"}); err == nil {
		t.Fatal("expected error for missing envelope")
	}
}

func TestOpenStaticBearerToken(t *testing.T) {
	svc := newTestSecretsService(t)
	injector := &Injector{secretSvc: svc}
	credential := &db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_test",
		ExternalID:       "cred_test",
	}
	payload, err := json.Marshal(map[string]any{"type": "static_bearer", "token": "super-secret-token"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	credential.SecretPayload = payload
	if err := SealCredentialSecret(context.Background(), svc, credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	credential.SecretPayload = json.RawMessage(`{"caller":"owned"}`)

	token, err := injector.openStaticBearerToken(context.Background(), credential)
	if err != nil {
		t.Fatalf("openStaticBearerToken: %v", err)
	}
	if token != "super-secret-token" {
		t.Fatalf("token = %q", token)
	}
	if string(credential.SecretPayload) != `{"caller":"owned"}` {
		t.Fatalf("caller payload was modified: %s", credential.SecretPayload)
	}
}

func newTestSecretsService(t *testing.T) *secrets.Service {
	t.Helper()
	kek, err := secrets.GenerateKEK()
	if err != nil {
		t.Fatalf("generate KEK: %v", err)
	}
	svc, err := secrets.NewLocalService(context.Background(), kek)
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	return svc
}
