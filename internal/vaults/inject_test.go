package vaults

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

func TestOpenStaticBearerToken(t *testing.T) {
	svc := newTestSecretsService(t)
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

	token, err := openStaticBearerToken(context.Background(), svc, credential)
	if err != nil {
		t.Fatalf("openStaticBearerToken: %v", err)
	}
	if token != "super-secret-token" {
		t.Fatalf("token = %q", token)
	}
	if len(credential.SecretPayload) != 0 {
		t.Fatal("plaintext should be cleared after openStaticBearerToken")
	}
}

func TestOpenStaticBearerTokenErrors(t *testing.T) {
	svc := newTestSecretsService(t)
	if _, err := openStaticBearerToken(context.Background(), svc, &db.VaultCredential{ExternalID: "cred_missing"}); err == nil {
		t.Fatal("expected error for missing envelope")
	}
	if _, err := openStaticBearerToken(context.Background(), nil, &db.VaultCredential{
		ExternalID:     "cred_x",
		SecretEnvelope: &secrets.Envelope{Ciphertext: []byte("x"), Nonce: []byte("n"), WrappedDEK: []byte("w"), FormatVersion: 1, KeyProvider: "local", KeyVersion: 1},
	}); err == nil {
		t.Fatal("expected error when secrets service is nil")
	}
}

func TestRewriteAuthorizationNilInjector(t *testing.T) {
	var injector *Injector
	header := make(http.Header)
	header.Set("Authorization", "Bearer client-token")
	if err := injector.RewriteAuthorization(
		context.Background(),
		"cse_test",
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		mustURL(t, "https://mcp.example.com/mcp"),
		header,
	); err != nil {
		t.Fatalf("RewriteAuthorization: %v", err)
	}
	if got := header.Get("Authorization"); got != "Bearer client-token" {
		t.Fatalf("Authorization = %q, want client token preserved", got)
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
