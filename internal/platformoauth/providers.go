package platformoauth

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type Resolver struct {
	db        *db.DB
	secretSvc *secrets.Service
	feishu    config.FeishuOAuthClientConfig
}

func NewResolver(database *db.DB, secretSvc *secrets.Service, feishu config.FeishuOAuthClientConfig) *Resolver {
	return &Resolver{db: database, secretSvc: secretSvc, feishu: feishu}
}

func ProviderSecretBinding(providerExternalID string) secrets.Binding {
	return secrets.Binding{
		OrganizationUUID:     "platform",
		WorkspaceUUID:        "platform",
		VaultExternalID:      "platform_oauth_providers",
		CredentialExternalID: providerExternalID,
	}
}

func (r *Resolver) ResolveEnvOAuthProvider(ctx context.Context, providerKey string) (vaults.EnvOAuthProviderSnapshot, error) {
	if vaults.IsBuiltinEnvOAuthProviderKey(providerKey) {
		return vaults.FeishuEnvOAuthProvider(r.feishu.ClientID, r.feishu.ClientSecret), nil
	}
	custom, externalID, err := vaults.ParseEnvOAuthProviderKey(providerKey)
	if err != nil || !custom {
		return vaults.EnvOAuthProviderSnapshot{}, errors.Join(vaults.ErrEnvOAuthProviderUnavailable, err)
	}
	if r.db == nil {
		return vaults.EnvOAuthProviderSnapshot{}, vaults.ErrEnvOAuthProviderUnavailable
	}
	provider, err := r.db.GetPlatformOAuthProviderByExternalID(ctx, externalID)
	if err != nil {
		return vaults.EnvOAuthProviderSnapshot{}, errors.Join(vaults.ErrEnvOAuthProviderUnavailable, err)
	}
	clientSecret, err := r.openClientSecret(ctx, provider)
	if err != nil {
		return vaults.EnvOAuthProviderSnapshot{}, errors.Join(vaults.ErrEnvOAuthProviderUnavailable, err)
	}
	return CustomProviderSnapshot(provider, clientSecret), nil
}

func CustomProviderSnapshot(provider db.PlatformOAuthProvider, clientSecret string) vaults.EnvOAuthProviderSnapshot {
	return vaults.EnvOAuthProviderSnapshot{
		ProviderKey:             vaults.EnvOAuthCustomProviderKey(provider.ExternalID),
		Name:                    provider.Name,
		Builtin:                 false,
		AuthorizeEndpoint:       provider.AuthorizeEndpoint,
		TokenEndpoint:           provider.TokenEndpoint,
		ClientID:                provider.ClientID,
		ClientSecret:            clientSecret,
		Scope:                   provider.Scope,
		TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   provider.TokenEndpointEncoding,
	}
}

func (r *Resolver) ListProviders(ctx context.Context) ([]vaults.EnvOAuthProviderSnapshot, error) {
	providers := []vaults.EnvOAuthProviderSnapshot{vaults.FeishuEnvOAuthProvider(r.feishu.ClientID, r.feishu.ClientSecret)}
	if r.db == nil {
		return providers, nil
	}
	rows, err := r.db.ListPlatformOAuthProviders(ctx)
	if err != nil {
		return nil, err
	}
	for _, provider := range rows {
		providers = append(providers, CustomProviderSnapshot(provider, ""))
	}
	return providers, nil
}

func (r *Resolver) openClientSecret(ctx context.Context, provider db.PlatformOAuthProvider) (string, error) {
	if r.secretSvc == nil || provider.ClientSecretEnvelope == nil {
		return "", errors.New("platform oauth provider client secret is unavailable")
	}
	plaintext, err := r.secretSvc.Open(ctx, ProviderSecretBinding(provider.ExternalID), *provider.ClientSecretEnvelope)
	if err != nil {
		return "", err
	}
	defer clear(plaintext)
	return string(plaintext), nil
}
