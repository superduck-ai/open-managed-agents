package channels

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestEnvOAuthProviderReadyAllowsUnrestrictedWithoutHosts(t *testing.T) {
	provider := vaults.EnvOAuthProviderSnapshot{Enabled: true, Configured: true, DefaultSecretName: "FRIDAY_TOKEN", NetworkingType: "unrestricted"}
	if !envOAuthProviderReady(provider, "FRIDAY_TOKEN") || envOAuthNetworking(provider) != "unrestricted" {
		t.Fatal("unrestricted inline provider was rejected")
	}
	provider.NetworkingType = "limited"
	if envOAuthProviderReady(provider, "FRIDAY_TOKEN") || envOAuthNetworking(provider) != "limited" {
		t.Fatal("limited provider without hosts was accepted")
	}
	provider.DefaultAllowedHosts = []string{"friday.example"}
	if !envOAuthProviderReady(provider, "FRIDAY_TOKEN") {
		t.Fatal("limited provider with hosts was rejected")
	}
}
