package vaults

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestListInjectableMatches(t *testing.T) {
	bearerA := credential("static_bearer", "https://mcp.example.com/mcp", "vlt_a")
	bearerWrongPath := credential("static_bearer", "https://mcp.example.com/other", "vlt_b")
	oauthSameHost := credential("mcp_oauth", "https://mcp.example.com/mcp", "vlt_o")

	cases := []struct {
		name        string
		url         string
		creds       []db.VaultCredential
		wantErr     bool
		wantCovered bool
		wantIDs     []string
	}{
		{"malformed auth rejected", "https://mcp.example.com/mcp", []db.VaultCredential{{Auth: json.RawMessage(`{"type":"static_bearer","mcp_server_url":42}`)}}, true, false, nil},
		{"non-segment prefix no match", "https://mcp.example.com/mcp-admin", []db.VaultCredential{bearerA}, false, true, nil},
		{"same host without path match", "https://mcp.example.com/admin", []db.VaultCredential{bearerA}, false, true, nil},
		{"scheme mismatch uncovered", "http://mcp.example.com:443/mcp", []db.VaultCredential{bearerA}, false, false, nil},
		{"host mismatch uncovered", "https://other.example.com/mcp", []db.VaultCredential{bearerA}, false, false, nil},
		{"uncovered host", "https://registry.npmjs.org/pkg", []db.VaultCredential{bearerA}, false, false, nil},
		{"empty credentials", "https://mcp.example.com/mcp", nil, false, false, nil},
		{"first matching static_bearer wins order", "https://mcp.example.com/mcp/sse", []db.VaultCredential{bearerWrongPath, bearerA}, false, true, []string{"vlt_a"}},
		{"exact path", "https://mcp.example.com/mcp", []db.VaultCredential{bearerA}, false, true, []string{"vlt_a"}},
		{"root path covers host", "https://mcp.example.com/anything", []db.VaultCredential{credential("static_bearer", "https://mcp.example.com/", "vlt_root")}, false, true, []string{"vlt_root"}},
		{"skip non-injectable env then match", "https://mcp.example.com/mcp", []db.VaultCredential{credential("environment_variable", "https://mcp.example.com/mcp", "vlt_env"), bearerA}, false, true, []string{"vlt_a"}},
		{"inject mcp_oauth", "https://mcp.example.com/mcp", []db.VaultCredential{oauthSameHost}, false, true, []string{"vlt_o"}},
		{"mcp_oauth before later static_bearer", "https://mcp.example.com/mcp", []db.VaultCredential{oauthSameHost, bearerA}, false, true, []string{"vlt_o", "vlt_a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matches, hostCovered, err := listInjectableMatches(mustURL(t, tc.url), tc.creds)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if hostCovered != tc.wantCovered {
				t.Fatalf("hostCovered = %v, want %v", hostCovered, tc.wantCovered)
			}
			if len(matches) != len(tc.wantIDs) {
				t.Fatalf("matches len = %d, want %d", len(matches), len(tc.wantIDs))
			}
			for i, id := range tc.wantIDs {
				if matches[i].ExternalID != id {
					t.Fatalf("matches[%d] = %q, want %q", i, matches[i].ExternalID, id)
				}
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
