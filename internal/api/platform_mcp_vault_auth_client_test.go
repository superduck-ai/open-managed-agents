package api

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestResolveMCPVaultOAuthClientCredentialsBYOOverridesPlatform(t *testing.T) {
	clients := []config.PlatformOAuthClientConfig{{
		MCPServerURL: "https://api.githubcopilot.com/mcp/",
		ClientID:     "platform-id",
		ClientSecret: "platform-secret",
	}}

	id, secret := resolveMCPVaultOAuthClientCredentials(
		"byo-id",
		"byo-secret",
		clients,
		"https://api.githubcopilot.com/mcp/",
	)
	if id != "byo-id" || secret != "byo-secret" {
		t.Fatalf("BYO should win: id=%q secret=%q", id, secret)
	}

	id, secret = resolveMCPVaultOAuthClientCredentials(
		"",
		"",
		clients,
		"https://api.githubcopilot.com/mcp/",
	)
	if id != "platform-id" || secret != "platform-secret" {
		t.Fatalf("platform registry hit: id=%q secret=%q", id, secret)
	}

	id, secret = resolveMCPVaultOAuthClientCredentials("", "", clients, "https://other.example/mcp")
	if id != "" || secret != "" {
		t.Fatalf("no hit should leave empty for DCR: id=%q secret=%q", id, secret)
	}
}
