package networkpolicy

import (
	"encoding/json"
	"testing"
)

func TestSessionMCPHostValidation(t *testing.T) {
	for _, tc := range []struct {
		name, networking, endpoint string
		allowed                    bool
	}{
		{"default denied", `{"type":"limited","allowed_hosts":[]}`, "https://api.example.com/mcp", false},
		{"package manager does not grant MCP", `{"type":"limited","allowed_hosts":[],"allow_package_managers":true}`, "https://pypi.org/mcp", false},
		{"port mismatch", `{"type":"limited","allowed_hosts":["api.example.com:8443"]}`, "https://api.example.com/mcp", false},
		{"wildcard excludes apex", `{"type":"limited","allowed_hosts":["*.example.com"]}`, "https://example.com/mcp", false},
		{"lookalike host", `{"type":"limited","allowed_hosts":["example.com"]}`, "https://example.com.evil.test/mcp", false},
		{"normalized host", `{"type":"limited","allowed_hosts":["api.example.com"]}`, "https://API.Example.COM./mcp", true},
		{"wildcard subdomain", `{"type":"limited","allowed_hosts":["*.example.com"]}`, "https://api.example.com/mcp", true},
		{"explicit port", `{"type":"limited","allowed_hosts":["api.example.com:8443"]}`, "https://api.example.com:8443/mcp", true},
		{"HTTP default port", `{"type":"limited","allowed_hosts":["api.example.com:80"]}`, "http://api.example.com/mcp", true},
		{"MCP exception", `{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}`, "https://api.example.com/mcp", true},
		{"unrestricted", `{"type":"unrestricted"}`, "https://api.example.com/mcp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot, err := json.Marshal(map[string]any{"mcp_servers": []map[string]string{{"name": "peer", "type": "url", "url": tc.endpoint}}})
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateSessionMCPHosts(json.RawMessage(`{"type":"cloud","networking":`+tc.networking+`}`), snapshot)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
	for _, tc := range []struct{ config, snapshot string }{
		{`{"type":"cloud","networking":{"type":"unknown"}}`, `{}`},
		{`{"type":"cloud","networking":{"type":"limited"}}`, `{"mcp_servers":null}`},
		{`{"type":"cloud","networking":{"type":"limited"}}`, `{"mcp_servers":[{"type":"url","url":"not-url"}]}`},
	} {
		if ValidateSessionMCPHosts(json.RawMessage(tc.config), json.RawMessage(tc.snapshot)) == nil {
			t.Fatal("malformed input accepted", tc)
		}
	}
	if err := ValidateSessionMCPHosts(json.RawMessage(`{"type":"cloud","networking":{"type":"limited"}}`), json.RawMessage(`{"mcp_servers":[]}`)); err != nil {
		t.Fatal("empty MCP declaration rejected", err)
	}
}
