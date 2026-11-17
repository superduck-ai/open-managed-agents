package api

import (
	"context"
	"errors"

	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func (s *Server) resolveEnvOAuthStartProvider(ctx context.Context, req *envOAuthVaultAuthStartRequest, workspaceUUID, vaultID string) (vaultsapi.EnvOAuthProviderSnapshot, error) {
	if req.ProviderID != vaultsapi.EnvOAuthProviderInline {
		if req.CustomProvider != nil {
			return vaultsapi.EnvOAuthProviderSnapshot{}, errors.New("custom_provider requires an inline OAuth provider")
		}
		return s.envOAuthProviders.ResolveEnvOAuthProvider(ctx, req.ProviderID)
	}
	if req.CustomProvider != nil {
		if err := vaultsapi.NormalizeEnvOAuthCustomProvider(req.ProviderID, req.CustomProvider); err != nil {
			return vaultsapi.EnvOAuthProviderSnapshot{}, err
		}
	}
	var version int64
	if req.CredentialID != "" {
		current, err := s.db.GetVaultCredential(ctx, workspaceUUID, vaultID, req.CredentialID)
		if err != nil {
			return vaultsapi.EnvOAuthProviderSnapshot{}, err
		}
		version = current.SecretVersion
		if req.CustomProvider == nil || req.CustomProvider.ClientSecret == "" {
			stored, err := vaultsapi.OpenEnvOAuthCustomProvider(ctx, s.vaultSecrets, current)
			if err != nil {
				return vaultsapi.EnvOAuthProviderSnapshot{}, err
			}
			if req.CustomProvider == nil {
				req.CustomProvider = stored
			} else if req.CustomProvider.ClientSecret == "" && req.CustomProvider.ClientID == stored.ClientID {
				req.CustomProvider.ClientSecret = stored.ClientSecret
			}
		}
	}
	if req.CustomProvider == nil {
		return vaultsapi.EnvOAuthProviderSnapshot{}, errors.New("custom_provider is required")
	}
	custom := req.CustomProvider
	if custom.ClientID == "" || custom.ClientSecret == "" {
		return vaultsapi.EnvOAuthProviderSnapshot{}, errors.New("custom OAuth authorization requires client ID and secret")
	}
	return vaultsapi.EnvOAuthProviderSnapshot{Enabled: true, Version: version, ProviderKey: vaultsapi.EnvOAuthProviderInline, Name: custom.Name, AuthorizeEndpoint: custom.AuthorizeEndpoint, TokenEndpoint: custom.TokenEndpoint, Scope: custom.Scope, ClientID: custom.ClientID, ClientSecret: custom.ClientSecret, TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "form"}, nil
}
