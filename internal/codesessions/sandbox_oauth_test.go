package codesessions

import (
	"context"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type sandboxOAuthDirectory struct {
	providers []vaults.EnvOAuthProviderSnapshot
}

func (d sandboxOAuthDirectory) ListProviders(context.Context) ([]vaults.EnvOAuthProviderSnapshot, error) {
	return d.providers, nil
}
func (d sandboxOAuthDirectory) ResolveEnvOAuthProvider(context.Context, string) (vaults.EnvOAuthProviderSnapshot, error) {
	return d.providers[0], nil
}

func TestSandboxOAuthMatchesConfiguredClient(t *testing.T) {
	provider := vaults.EnvOAuthProviderSnapshot{ProviderKey: "custom:catalog", Enabled: true, Configured: true, DefaultSecretName: "CATALOG_TOKEN", DefaultAllowedHosts: []string{"catalog.example"}}
	input := OAuthRequest{Kind: "env_oauth", SecretName: "CATALOG_TOKEN", TargetHost: "catalog.example"}
	wildcard := provider
	wildcard.DefaultAllowedHosts = []string{"*.example"}
	withPort := provider
	withPort.DefaultAllowedHosts = []string{"catalog.example:443"}
	disabled := provider
	disabled.Enabled = false
	for _, test := range []struct {
		name      string
		providers []vaults.EnvOAuthProviderSnapshot
		workspace string
		valid     bool
	}{
		{"唯一客户端", []vaults.EnvOAuthProviderSnapshot{provider}, "workspace", true},
		{"通配域名", []vaults.EnvOAuthProviderSnapshot{wildcard}, "workspace", true},
		{"允许端口", []vaults.EnvOAuthProviderSnapshot{withPort}, "workspace", true},
		{"已停用客户端", []vaults.EnvOAuthProviderSnapshot{disabled}, "workspace", false},
		{"没有客户端", nil, "workspace", false},
		{"多个客户端", []vaults.EnvOAuthProviderSnapshot{provider, provider}, "workspace", false},
		{"平台客户端跨工作区", []vaults.EnvOAuthProviderSnapshot{provider}, "other", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := &Handler{envOAuthProviders: sandboxOAuthDirectory{test.providers}}
			got, err := h.resolveSandboxOAuthProvider(t.Context(), input, nil)
			if (err == nil) != test.valid || test.valid && got.ProviderKey != provider.ProviderKey {
				t.Fatalf("客户端匹配错误：%v", err)
			}
		})
	}
}
