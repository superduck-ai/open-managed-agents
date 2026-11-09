package config

import "testing"

func TestFindPlatformOAuthClientExactMatch(t *testing.T) {
	clients := []PlatformOAuthClientConfig{
		{
			MCPServerURL: "https://api.githubcopilot.com/mcp/",
			ClientID:     "platform-github",
			ClientSecret: "platform-secret",
		},
		{
			MCPServerURL: " https://mcp.example.com/mcp ",
			ClientID:     "platform-example",
			ClientSecret: "example-secret",
		},
	}

	got, ok := FindPlatformOAuthClient(clients, "https://api.githubcopilot.com/mcp/")
	if !ok {
		t.Fatal("expected GitHub Platform OAuth Client hit")
	}
	if got.ClientID != "platform-github" || got.ClientSecret != "platform-secret" {
		t.Fatalf("unexpected client: %+v", got)
	}

	got, ok = FindPlatformOAuthClient(clients, "https://mcp.example.com/mcp")
	if !ok || got.ClientID != "platform-example" {
		t.Fatalf("trim match failed: ok=%t got=%+v", ok, got)
	}

	if _, ok := FindPlatformOAuthClient(clients, "https://api.githubcopilot.com/mcp"); ok {
		t.Fatal("exact match must not ignore trailing slash")
	}
	if _, ok := FindPlatformOAuthClient(clients, ""); ok {
		t.Fatal("empty mcp_server_url must miss")
	}
}

func TestFindPlatformOAuthClientTrimsClientCredentials(t *testing.T) {
	clients := []PlatformOAuthClientConfig{{
		MCPServerURL: " https://api.githubcopilot.com/mcp/ ",
		ClientID:     " platform-id ",
		ClientSecret: " platform-secret ",
	}}

	got, ok := FindPlatformOAuthClient(clients, "https://api.githubcopilot.com/mcp/")
	if !ok {
		t.Fatal("expected Platform OAuth Client hit")
	}
	if got.MCPServerURL != "https://api.githubcopilot.com/mcp/" {
		t.Fatalf("MCPServerURL = %q, want trimmed exact url", got.MCPServerURL)
	}
	if got.ClientID != "platform-id" {
		t.Fatalf("ClientID = %q, want trimmed value", got.ClientID)
	}
	if got.ClientSecret != "platform-secret" {
		t.Fatalf("ClientSecret = %q, want trimmed value", got.ClientSecret)
	}
}

func TestValidatePlatformOAuthClients(t *testing.T) {
	t.Run("empty ok", func(t *testing.T) {
		if err := validatePlatformOAuthClients(nil); err != nil {
			t.Fatalf("nil registry: %v", err)
		}
	})
	t.Run("requires url and client_id", func(t *testing.T) {
		err := validatePlatformOAuthClients([]PlatformOAuthClientConfig{{
			ClientID: "only-id",
		}})
		if err == nil {
			t.Fatal("expected missing mcp_server_url error")
		}
		err = validatePlatformOAuthClients([]PlatformOAuthClientConfig{{
			MCPServerURL: "https://api.githubcopilot.com/mcp/",
		}})
		if err == nil {
			t.Fatal("expected missing client_id error")
		}
	})
	t.Run("rejects duplicate urls", func(t *testing.T) {
		err := validatePlatformOAuthClients([]PlatformOAuthClientConfig{
			{MCPServerURL: "https://api.githubcopilot.com/mcp/", ClientID: "a"},
			{MCPServerURL: " https://api.githubcopilot.com/mcp/ ", ClientID: "b"},
		})
		if err == nil {
			t.Fatal("expected duplicate mcp_server_url error")
		}
	})
	t.Run("allows empty client_secret", func(t *testing.T) {
		err := validatePlatformOAuthClients([]PlatformOAuthClientConfig{{
			MCPServerURL: "https://api.githubcopilot.com/mcp/",
			ClientID:     "Iv23lihgPciL8cLgFnA2",
		}})
		if err != nil {
			t.Fatalf("empty secret should be allowed: %v", err)
		}
	})
}
