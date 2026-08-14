package vaults

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSubstituteEnvSecretsHeaderAndBody(t *testing.T) {
	t.Parallel()
	svc := newTestSecretsService(t)
	auth, _ := json.Marshal(map[string]any{
		"type":               "environment_variable",
		"secret_name":        "NOTION_API_KEY",
		"placeholder":        "oma_ph_a",
		"networking":         map[string]any{"type": "limited", "allowed_hosts": []string{"api.notion.com"}},
		"injection_location": map[string]any{"header": true, "body": true},
	})
	secret, _ := json.Marshal(map[string]any{"type": "environment_variable", "secret_value": "ntn_real"})
	credential := db.VaultCredential{
		OrganizationUUID: "00000000-0000-0000-0000-000000000001",
		WorkspaceUUID:    "00000000-0000-0000-0000-000000000002",
		VaultExternalID:  "vlt_a",
		ExternalID:       "cred_a",
		AuthType:         "environment_variable",
		Auth:             auth,
		SecretPayload:    secret,
	}
	if err := SealCredentialSecret(context.Background(), svc, &credential); err != nil {
		t.Fatalf("seal: %v", err)
	}
	substitutor := NewEgressSubstitutor(nil, svc, nil)
	substitutor.store = &fakeCredentialStore{
		vaultIDs:    []string{"vlt_a"},
		credentials: []db.VaultCredential{credential},
	}
	req, err := http.NewRequest(http.MethodPost, "https://api.notion.com/v1", strings.NewReader(`{"token":"oma_ph_a"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer oma_ph_a")

	if err := substitutor.SubstituteEnvSecrets(
		context.Background(),
		"cse_test",
		credential.OrganizationUUID,
		credential.WorkspaceUUID,
		req,
		"api.notion.com",
		"443",
	); err != nil {
		t.Fatalf("SubstituteEnvSecrets: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer ntn_real" {
		t.Fatalf("Authorization = %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"token":"ntn_real"}` {
		t.Fatalf("body = %s", body)
	}
}
