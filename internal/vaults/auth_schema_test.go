package vaults

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCredentialAuthUnmarshalRejectsInvalidSchemas(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "invalid JSON", raw: `{"type":`},
		{name: "missing type", raw: `{}`},
		{name: "unsupported type", raw: `{"type":"unknown"}`},
		{name: "invalid concrete field", raw: `{"type":"static_bearer","mcp_server_url":42}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var auth credentialAuth
			if err := json.Unmarshal([]byte(test.raw), &auth); err == nil {
				t.Fatal("expected auth schema error")
			}
		})
	}
}

func TestCredentialAuthUnmarshalDispatchesByType(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		assertAuth func(*testing.T, credentialAuth)
	}{
		{
			name: "mcp oauth",
			raw: `{
				"type":"mcp_oauth",
				"mcp_server_url":"https://mcp.example.com/mcp",
				"expires_at":"2099-12-31T23:59:59Z",
				"refresh":{
					"token_endpoint":"https://example.com/token",
					"client_id":"client-id",
					"token_endpoint_auth":{"type":"none"},
					"scope":"read"
				}
			}`,
			assertAuth: func(t *testing.T, auth credentialAuth) {
				value, ok := auth.value.(*mcpOAuthCredentialAuth)
				if !ok {
					t.Fatalf("value type = %T", auth.value)
				}
				if value.MCPServerURL != "https://mcp.example.com/mcp" || value.Refresh == nil || value.Refresh.ClientID != "client-id" {
					t.Fatalf("unexpected mcp oauth auth: %+v", value)
				}
			},
		},
		{
			name: "static bearer",
			raw:  `{"type":"static_bearer","mcp_server_url":"https://mcp.example.com/sse"}`,
			assertAuth: func(t *testing.T, auth credentialAuth) {
				value, ok := auth.value.(*staticBearerCredentialAuth)
				if !ok {
					t.Fatalf("value type = %T", auth.value)
				}
				if value.MCPServerURL != "https://mcp.example.com/sse" {
					t.Fatalf("mcp_server_url = %q", value.MCPServerURL)
				}
			},
		},
		{
			name: "environment variable",
			raw:  `{"type":"environment_variable","secret_name":"NOTION_TOKEN","networking":{"type":"limited","allowed_hosts":["api.notion.com"]}}`,
			assertAuth: func(t *testing.T, auth credentialAuth) {
				value, ok := auth.value.(*environmentVariableCredentialAuth)
				if !ok {
					t.Fatalf("value type = %T", auth.value)
				}
				if value.SecretName != "NOTION_TOKEN" || value.Networking.AllowedHosts == nil || len(*value.Networking.AllowedHosts) != 1 {
					t.Fatalf("unexpected environment variable auth: %+v", value)
				}
			},
		},
		{
			name: "env oauth",
			raw:  `{"type":"env_oauth","provider":"builtin:feishu","secret_name":"LARKSUITE_CLI_USER_ACCESS_TOKEN","placeholder":"oma_ph_abc","networking":{"type":"limited","allowed_hosts":["open.feishu.cn"]},"injection_location":{"header":true,"body":false},"expires_at":"2099-12-31T23:59:59Z","has_refresh_token":true}`,
			assertAuth: func(t *testing.T, auth credentialAuth) {
				value, ok := auth.value.(*envOAuthCredentialAuth)
				if !ok {
					t.Fatalf("value type = %T", auth.value)
				}
				if value.Provider != "builtin:feishu" || value.SecretName != EnvOAuthFeishuDefaultSecretName || !value.HasRefreshToken {
					t.Fatalf("unexpected env oauth auth: %+v", value)
				}
				if value.InjectionLocation.Body {
					t.Fatalf("env oauth injection_location = %+v, want header only", value.InjectionLocation)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var auth credentialAuth
			if err := json.Unmarshal([]byte(test.raw), &auth); err != nil {
				t.Fatalf("unmarshal auth: %v", err)
			}
			test.assertAuth(t, auth)

			encoded, err := json.Marshal(auth)
			if err != nil {
				t.Fatalf("marshal auth: %v", err)
			}
			if strings.Contains(string(encoded), `"value"`) {
				t.Fatalf("auth wrapper leaked into JSON: %s", encoded)
			}
		})
	}
}
