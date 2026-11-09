package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestResolveMCPVaultOAuthClientCredentialsBYOOverridesPlatform(t *testing.T) {
	clients := []config.PlatformOAuthClientConfig{{
		MCPServerURL: "https://api.githubcopilot.com/mcp/",
		ClientID:     "platform-id",
		ClientSecret: "platform-secret",
	}}

	id, secret, source := resolveMCPVaultOAuthClientCredentials(
		"byo-id",
		"byo-secret",
		clients,
		"https://api.githubcopilot.com/mcp/",
	)
	if id != "byo-id" || secret != "byo-secret" || source != vaultsapi.MCPOAuthClientCredentialSealed {
		t.Fatalf("BYO should win: id=%q secret=%q source=%q", id, secret, source)
	}

	id, secret, source = resolveMCPVaultOAuthClientCredentials(
		"",
		"",
		clients,
		"https://api.githubcopilot.com/mcp/",
	)
	if id != "platform-id" || secret != "platform-secret" || source != vaultsapi.MCPOAuthClientCredentialPlatform {
		t.Fatalf("platform registry miss: id=%q secret=%q source=%q", id, secret, source)
	}

	id, secret, source = resolveMCPVaultOAuthClientCredentials("", "", clients, "https://other.example/mcp")
	if id != "" || secret != "" || source != vaultsapi.MCPOAuthClientCredentialSealed {
		t.Fatalf("no hit should leave empty for DCR: id=%q secret=%q source=%q", id, secret, source)
	}
}

func TestPlatformMCPOAuthCredentialURLSeparation(t *testing.T) {
	const service = "https://bridge.example"
	const target = "https://open.feishu.cn/open-apis"
	for _, targetURL := range []string{service, target} {
		flow := db.MCPOAuthFlow{MCPServerURL: service, TokenEndpoint: service + "/token"}
		raw, _, err := buildPlatformMCPVaultOAuthCredentialPayloads(flow, config.OAuthCredentialTargetURL([]config.OAuthServiceBindingConfig{{ServiceURL: service, CredentialTargetURL: targetURL}}, service), vaultsapi.OAuthTokenEndpointResult{AccessToken: "access"}, time.Now(), "")
		if err != nil {
			t.Fatal(err)
		}
		var auth struct {
			MCPServerURL    string `json:"mcp_server_url"`
			OAuthServiceURL string `json:"oauth_service_url"`
		}
		if err := json.Unmarshal(raw, &auth); err != nil {
			t.Fatal(err)
		}
		if auth.MCPServerURL != targetURL {
			t.Fatal("incorrect credential target")
		}
		if targetURL == service && auth.OAuthServiceURL != "" {
			t.Fatal("legacy flow changed")
		}
		if targetURL != service && auth.OAuthServiceURL != service {
			t.Fatal("missing service identity")
		}
	}
}
