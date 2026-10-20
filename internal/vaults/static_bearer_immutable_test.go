package vaults

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"strings"
	"testing"
)

func TestStaticBearerUpdateRejectsStructuralURL(t *testing.T) {
	current := db.VaultCredential{AuthType: "static_bearer", CredentialKey: "https://original.test/mcp", Auth: json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://original.test/mcp"}`)}
	original := []byte(`{"type":"static_bearer","token":"local-original"}`)
	for _, value := range []any{"https://changed.test/mcp", "https://original.test/mcp", "", nil, 42} {
		raw, _ := json.Marshal(map[string]any{"type": "static_bearer", "mcp_server_url": value, "token": "local-replacement"})
		_, err := normalizeCredentialAuthForUpdate(current, original, raw)
		if err == nil || !strings.Contains(err.Error(), "mcp_server_url") {
			t.Fatal("structural URL update was not rejected")
		}
	}
	state, err := normalizeCredentialAuthForUpdate(current, original, json.RawMessage(`{"type":"static_bearer","token":"local-replacement"}`))
	if err != nil {
		t.Fatal(err)
	}
	if state.Key != current.CredentialKey || !strings.Contains(string(state.PublicAuth), `"mcp_server_url":"https://original.test/mcp"`) {
		t.Fatal("secret-only rotation changed structural URL")
	}
	secret, err := decodeStaticBearerCredentialSecret(state.SecretPayload)
	if err != nil || secret.Token != "local-replacement" {
		t.Fatal("secret-only rotation failed")
	}
}
