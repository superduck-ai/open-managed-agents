package api

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
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
