package vaults

import (
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestInlineOAuthCredentialResolvesWithoutProviderDirectory(t *testing.T) {
	svc := newTestSecretsService(t)
	custom := &EnvOAuthCustomProvider{
		EnvOAuthProviderDefinition: EnvOAuthProviderDefinition{Name: "Catalog", AuthorizeEndpoint: "https://catalog.example/authorize", TokenEndpoint: "https://catalog.example/token", ClientID: "client", TokenEndpointAuthMethod: "client_secret_basic"},
		ClientSecret:               "secret",
	}
	public, secret, err := BuildEnvOAuthCredentialPayloads(EnvOAuthCredentialBuildInput{
		Provider: EnvOAuthProviderInline, CustomProvider: custom, SecretName: "CATALOG_TOKEN", AccessToken: "expired",
		NetworkingType: "limited", AllowedHosts: []string{"catalog.example"}, Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := db.VaultCredential{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "vlt_personal", ExternalID: "vcrd_catalog", AuthType: "env_oauth", CredentialKey: "CATALOG_TOKEN", Auth: public, SecretPayload: secret, SecretVersion: 7}
	if err := SealCredentialSecret(t.Context(), svc, &credential); err != nil {
		t.Fatal(err)
	}
	provider, err := ResolveEnvOAuthCredentialProvider(t.Context(), svc, credential, nil)
	if err != nil || provider.ProviderKey != EnvOAuthProviderInline || provider.ClientID != "client" || provider.ClientSecret != "secret" || provider.DefaultSecretName != "CATALOG_TOKEN" || provider.Version != 7 || provider.TokenEndpointAuthMethod != "client_secret_basic" || len(provider.DefaultAllowedHosts) != 1 || provider.DefaultAllowedHosts[0] != "catalog.example" {
		t.Fatalf("未复用凭据内的 OAuth 配置：%v", err)
	}
}

func TestInlineOAuthRejectsUnsupportedClientAuthentication(t *testing.T) {
	custom := &EnvOAuthCustomProvider{EnvOAuthProviderDefinition: EnvOAuthProviderDefinition{
		Name: "Catalog", AuthorizeEndpoint: "https://catalog.example/authorize", TokenEndpoint: "https://catalog.example/token", TokenEndpointAuthMethod: "private_key_jwt",
	}}
	if err := NormalizeEnvOAuthCustomProvider(EnvOAuthProviderInline, custom); err == nil {
		t.Fatal("接受了原生换票器不支持的认证方式")
	}
	custom.TokenEndpointAuthMethod = "client_secret_basic"
	if err := NormalizeEnvOAuthCustomProvider(EnvOAuthProviderInline, custom); err != nil {
		t.Fatal(err)
	}
}
