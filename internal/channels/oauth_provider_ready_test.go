package channels

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestEnvOAuthProviderReadyAllowsUnrestrictedWithoutHosts(t *testing.T) {
	provider := vaults.EnvOAuthProviderSnapshot{Enabled: true, Configured: true, DefaultSecretName: "CATALOG_TOKEN", NetworkingType: "unrestricted"}
	if !envOAuthProviderReady(provider, "CATALOG_TOKEN") || envOAuthNetworking(provider) != "unrestricted" {
		t.Fatal("unrestricted inline provider was rejected")
	}
	provider.NetworkingType = "limited"
	if envOAuthProviderReady(provider, "CATALOG_TOKEN") || envOAuthNetworking(provider) != "limited" {
		t.Fatal("limited provider without hosts was accepted")
	}
	provider.DefaultAllowedHosts = []string{"catalog.example"}
	if !envOAuthProviderReady(provider, "CATALOG_TOKEN") {
		t.Fatal("limited provider with hosts was rejected")
	}
}
