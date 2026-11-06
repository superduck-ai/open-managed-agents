package platformoauth

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestResolverBuiltinFeishu(t *testing.T) {
	t.Parallel()
	resolver := NewResolver(nil, nil, config.FeishuOAuthClientConfig{ClientID: "client", ClientSecret: "secret"})
	provider, err := resolver.ResolveEnvOAuthProvider(t.Context(), vaults.EnvOAuthProviderBuiltinFeishu)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !provider.Builtin || provider.ClientID != "client" || provider.ClientSecret != "secret" || provider.TokenEndpointEncoding != "json" {
		t.Fatalf("provider = %+v", provider)
	}
	if provider.DefaultSecretName != vaults.EnvOAuthFeishuDefaultSecretName {
		t.Fatalf("default secret name = %q", provider.DefaultSecretName)
	}

	disabled := NewResolver(nil, nil, config.FeishuOAuthClientConfig{})
	provider, err = disabled.ResolveEnvOAuthProvider(t.Context(), vaults.EnvOAuthProviderBuiltinFeishu)
	if err != nil || provider.ClientID != "" {
		t.Fatalf("disabled provider = %+v: %v", provider, err)
	}
}

func TestResolverRejectsUnknownProvider(t *testing.T) {
	t.Parallel()
	resolver := NewResolver(nil, nil, config.FeishuOAuthClientConfig{})
	for _, key := range []string{"", "unknown", "builtin:other", "custom:"} {
		if _, err := resolver.ResolveEnvOAuthProvider(t.Context(), key); !errors.Is(err, vaults.ErrEnvOAuthProviderUnavailable) {
			t.Fatalf("ResolveEnvOAuthProvider(%q) error = %v", key, err)
		}
	}
}
