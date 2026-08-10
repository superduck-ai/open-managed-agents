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
		{"inject first matching static_bearer", "https://mcp.example.com/mcp/sse", []db.VaultCredential{bearerWrongPath, bearerA}, injectionInject, "vlt_a"},
		{"exact path", "https://mcp.example.com/mcp", []db.VaultCredential{bearerA}, injectionInject, "vlt_a"},
		{"non-segment prefix rejected", "https://mcp.example.com/mcp-admin", []db.VaultCredential{bearerA}, injectionReject, ""},
		{"host mismatch passthrough", "https://other.example.com/mcp", []db.VaultCredential{bearerA}, injectionPassthrough, ""},
		{"root path covers host", "https://mcp.example.com/anything", []db.VaultCredential{credential("static_bearer", "https://mcp.example.com/", "vlt_root")}, injectionInject, "vlt_root"},
		{"passthrough when host uncovered", "https://registry.npmjs.org/pkg", []db.VaultCredential{bearerA}, injectionPassthrough, ""},
		{"reject same host without path match", "https://mcp.example.com/admin", []db.VaultCredential{bearerA}, injectionReject, ""},
		{"skip non-bearer then inject", "https://mcp.example.com/mcp", []db.VaultCredential{oauthSameHost, bearerA}, injectionInject, "vlt_a"},
		{"reject non-injectable only", "https://mcp.example.com/mcp", []db.VaultCredential{oauthSameHost}, injectionReject, ""},
		{"empty credentials passthrough", "https://mcp.example.com/mcp", nil, injectionPassthrough, ""},
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
	auth, _ := json.Marshal(map[string]any{"type": authType, "mcp_server_url": serverURL})
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
