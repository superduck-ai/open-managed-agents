package vaults

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialCreateRequiresExplicitRefreshAuthentication(t *testing.T) {
	for _, value := range []string{"", `,"token_endpoint_auth":null`} {
		raw := json.RawMessage(`{"type":"mcp_oauth","mcp_server_url":"http://owned.example.test/mcp","access_token":"synthetic-access","refresh":{"token_endpoint":"http://owned.example.test/token","client_id":"client","refresh_token":"synthetic-refresh"` + value + `}}`)
		state, err := normalizeCredentialAuthForCreate(raw)
		if err == nil {
			t.Fatal("missing refresh authentication accepted as none")
		}
		if !strings.Contains(err.Error(), "auth.refresh.token_endpoint_auth") {
			t.Fatal("missing field not identified")
		}
		if len(state.PublicAuth) != 0 || len(state.SecretPayload) != 0 {
			t.Fatal("invalid create produced credential state")
		}
	}
}

func TestCredentialCreateAcceptsExplicitRefreshAuthenticationBranches(t *testing.T) {
	for _, auth := range []string{`{"type":"none"}`, `{"type":"client_secret_basic","client_secret":"synthetic-client-secret"}`, `{"type":"client_secret_post","client_secret":"synthetic-client-secret"}`} {
		raw := json.RawMessage(`{"type":"mcp_oauth","mcp_server_url":"http://owned.example.test/mcp","access_token":"synthetic-access","refresh":{"token_endpoint":"http://owned.example.test/token","client_id":"client","refresh_token":"synthetic-refresh","token_endpoint_auth":` + auth + `}}`)
		state, err := normalizeCredentialAuthForCreate(raw)
		if err != nil {
			t.Fatal("valid explicit branch rejected", err)
		}
		if strings.Contains(string(state.PublicAuth), "synthetic-") || len(state.SecretPayload) == 0 {
			t.Fatal("write-only secret boundary changed")
		}
	}
}
