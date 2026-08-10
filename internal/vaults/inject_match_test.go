package vaults

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDecideInjection(t *testing.T) {
	bearerA := credential("static_bearer", "https://mcp.example.com/mcp", "vlt_a")
	bearerWrongPath := credential("static_bearer", "https://mcp.example.com/other", "vlt_b")
	oauthSameHost := credential("mcp_oauth", "https://mcp.example.com/mcp", "vlt_o")

	cases := []struct {
		name   string
		url    string
		creds  []db.VaultCredential
		want   injectionKind
		wantID string
	}{
		{"malformed auth rejected", "https://mcp.example.com/mcp", []db.VaultCredential{{Auth: json.RawMessage(`{"type":"static_bearer","mcp_server_url":42}`)}}, injectionReject, ""},
		{"non-segment prefix rejected", "https://mcp.example.com/mcp-admin", []db.VaultCredential{bearerA}, injectionReject, ""},
		{"reject same host without path match", "https://mcp.example.com/admin", []db.VaultCredential{bearerA}, injectionReject, ""},
		{"scheme mismatch passthrough", "http://mcp.example.com:443/mcp", []db.VaultCredential{bearerA}, injectionPassthrough, ""},
		{"host mismatch passthrough", "https://other.example.com/mcp", []db.VaultCredential{bearerA}, injectionPassthrough, ""},
		{"passthrough when host uncovered", "https://registry.npmjs.org/pkg", []db.VaultCredential{bearerA}, injectionPassthrough, ""},
		{"empty credentials passthrough", "https://mcp.example.com/mcp", nil, injectionPassthrough, ""},
		{"inject first matching static_bearer", "https://mcp.example.com/mcp/sse", []db.VaultCredential{bearerWrongPath, bearerA}, injectionInject, "vlt_a"},
		{"exact path", "https://mcp.example.com/mcp", []db.VaultCredential{bearerA}, injectionInject, "vlt_a"},
		{"root path covers host", "https://mcp.example.com/anything", []db.VaultCredential{credential("static_bearer", "https://mcp.example.com/", "vlt_root")}, injectionInject, "vlt_root"},
		{"skip non-injectable env then inject", "https://mcp.example.com/mcp", []db.VaultCredential{credential("environment_variable", "https://mcp.example.com/mcp", "vlt_env"), bearerA}, injectionInject, "vlt_a"},
		{"inject mcp_oauth", "https://mcp.example.com/mcp", []db.VaultCredential{oauthSameHost}, injectionInject, "vlt_o"},
		{"mcp_oauth before later static_bearer", "https://mcp.example.com/mcp", []db.VaultCredential{oauthSameHost, bearerA}, injectionInject, "vlt_o"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := decideInjection(mustURL(t, tc.url), tc.creds)
			if decision.Kind != tc.want {
				t.Fatalf("kind = %v, want %v", decision.Kind, tc.want)
			}
			if tc.wantID == "" {
				if decision.Credential != nil {
					t.Fatalf("credential = %+v, want nil", decision.Credential)
				}
				return
			}
			if decision.Credential == nil || decision.Credential.ExternalID != tc.wantID {
				t.Fatalf("credential = %+v, want %s", decision.Credential, tc.wantID)
			}
		})
	}
}

func credential(authType, serverURL, id string) db.VaultCredential {
	var auth []byte
	switch authType {
	case "environment_variable":
		auth, _ = json.Marshal(map[string]any{
			"type":        authType,
			"secret_name": "EXAMPLE_TOKEN",
			"networking":  map[string]any{"type": "unrestricted"},
		})
	default:
		auth, _ = json.Marshal(map[string]any{"type": authType, "mcp_server_url": serverURL})
	}
	return db.VaultCredential{ExternalID: id, AuthType: authType, Auth: auth}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return u
}
