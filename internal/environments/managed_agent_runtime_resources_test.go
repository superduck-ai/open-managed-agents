package environments

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestManagedAgentSourcesIncludeGitHubAuthorizationToken(t *testing.T) {
	// 失败场景先行：带 SecretPayload 的 github_repository → source 含 authorization_token
	resources := []db.SessionResource{
		{
			ResourceType:  "github_repository",
			Payload:       json.RawMessage(`{"type":"github_repository","url":"https://github.com/acme/private","mount_path":"/workspace/private"}`),
			SecretPayload: json.RawMessage(`{"authorization_token":"ghp_private_token"}`),
		},
	}
	sources := managedAgentRuntimeSourceValues(t, resolveManagedAgentRuntimeResources(resources).sources)
	if len(sources) != 1 {
		t.Fatalf("sources = %v, want 1", sources)
	}
	source := sources[0].(map[string]any)
	if source["authorization_token"] != "ghp_private_token" {
		t.Fatalf("authorization_token = %v, want ghp_private_token", source["authorization_token"])
	}
}

func TestManagedAgentSourcesOmitGitHubTokenWhenAbsent(t *testing.T) {
	// 无 SecretPayload → source 不含 authorization_token
	resources := []db.SessionResource{
		{
			ResourceType: "github_repository",
			Payload:      json.RawMessage(`{"type":"github_repository","url":"https://github.com/acme/public","mount_path":"/workspace/public"}`),
		},
	}
	sources := managedAgentRuntimeSourceValues(t, resolveManagedAgentRuntimeResources(resources).sources)
	if len(sources) != 1 {
		t.Fatalf("sources = %v, want 1", sources)
	}
	if _, ok := sources[0].(map[string]any)["authorization_token"]; ok {
		t.Fatalf("无 SecretPayload 时不应有 authorization_token: %v", sources[0])
	}
}

func TestGitHubAuthorizationTokenFromSecret(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{"with token", json.RawMessage(`{"authorization_token":"ghp_x"}`), "ghp_x"},
		{"empty", json.RawMessage(``), ""},
		{"null", json.RawMessage(`null`), ""},
		{"missing field", json.RawMessage(`{"other":"value"}`), ""},
		{"invalid", json.RawMessage(`not-json`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := githubAuthorizationTokenFromSecret(tc.raw); got != tc.want {
				t.Fatalf("githubAuthorizationTokenFromSecret(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
