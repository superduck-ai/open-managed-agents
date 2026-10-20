package vaults

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPURLUserinfoRejectedBeforePublicAuth(t *testing.T) {
	for _, kind := range []string{"static_bearer", "mcp_oauth"} {
		for _, userinfo := range []string{"user:synthetic-url-password@", "user@", ":synthetic-url-password@", "user:@"} {
			t.Run(kind+"/"+userinfo[:1], func(t *testing.T) {
				auth := map[string]string{"type": kind, "mcp_server_url": "https://" + userinfo + "mcp.example.test:8443/mcp", "token": "synthetic-credential-token", "access_token": "synthetic-access-token"}
				raw, err := json.Marshal(auth)
				if err != nil {
					t.Fatal("encode input failed")
				}
				state, err := normalizeCredentialAuthForCreate(raw)
				if err == nil {
					t.Fatal("credential URL userinfo was accepted into public auth")
				}
				if strings.Contains(err.Error(), "synthetic-url-password") || strings.Contains(err.Error(), "mcp.example.test") {
					t.Fatal("validation error echoed credential URL")
				}
				if len(state.PublicAuth) != 0 || len(state.SecretPayload) != 0 || state.Key != "" {
					t.Fatal("rejected URL produced public or secret credential state")
				}
			})
		}
	}
}

func TestMCPURLWithoutUserinfoKeepsExistingURLPolicy(t *testing.T) {
	for _, kind := range []string{"static_bearer", "mcp_oauth"} {
		for _, address := range []string{"http://mcp.example.test:6788/mcp", "https://mcp.example.test/mcp", "https://mcp.example.test/mcp?probe=test", "https://mcp.example.test/mcp#fragment", "http://[::1]:6788/mcp"} {
			t.Run(kind+"/"+address, func(t *testing.T) {
				raw, err := json.Marshal(map[string]string{"type": kind, "mcp_server_url": address, "token": "synthetic-credential-token", "access_token": "synthetic-access-token"})
				if err != nil {
					t.Fatal("encode input failed")
				}
				state, err := normalizeCredentialAuthForCreate(raw)
				if err != nil {
					t.Fatal("normal credential URL rejected")
				}
				var public struct {
					MCPServerURL string `json:"mcp_server_url"`
				}
				if err := json.Unmarshal(state.PublicAuth, &public); err != nil {
					t.Fatal("public auth decoding failed")
				}
				if public.MCPServerURL != address || state.Key != address || strings.Contains(string(state.PublicAuth), "synthetic-credential-token") || strings.Contains(string(state.PublicAuth), "synthetic-access-token") {
					t.Fatal("normal URL or write-only policy changed")
				}
			})
		}
	}
}
