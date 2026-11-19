package vaults

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

const EnvOAuthInlineNameMaxRunes = 200

type EnvOAuthProviderDefinition struct {
	Name                    string `json:"name"`
	AuthorizeEndpoint       string `json:"authorize_endpoint"`
	TokenEndpoint           string `json:"token_endpoint"`
	Scope                   string `json:"scope"`
	ClientID                string `json:"client_id,omitempty"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty"`
}

type EnvOAuthCustomProvider struct {
	EnvOAuthProviderDefinition
	ClientSecret string `json:"client_secret,omitempty"`
}

func NormalizeEnvOAuthCustomProvider(providerKey string, provider *EnvOAuthCustomProvider) error {
	if providerKey != EnvOAuthProviderInline {
		if provider != nil {
			return errors.New("custom_provider requires an inline OAuth provider")
		}
		return nil
	}
	if provider == nil {
		return errors.New("custom_provider is required")
	}
	provider.Name = strings.TrimSpace(provider.Name)
	provider.AuthorizeEndpoint = strings.TrimSpace(provider.AuthorizeEndpoint)
	provider.TokenEndpoint = strings.TrimSpace(provider.TokenEndpoint)
	provider.Scope = strings.TrimSpace(provider.Scope)
	provider.ClientID = strings.TrimSpace(provider.ClientID)
	provider.ClientSecret = strings.TrimSpace(provider.ClientSecret)
	if provider.Name == "" || utf8.RuneCountInString(provider.Name) > EnvOAuthInlineNameMaxRunes {
		return errors.New("custom_provider.name is required and must not exceed 200 characters")
	}
	if provider.TokenEndpointAuthMethod != "" && provider.TokenEndpointAuthMethod != "client_secret_post" && provider.TokenEndpointAuthMethod != "client_secret_basic" {
		return errors.New("custom OAuth client authentication must use client_secret_post or client_secret_basic")
	}
	for _, endpoint := range []string{provider.AuthorizeEndpoint, provider.TokenEndpoint} {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Scheme != "https" {
			return errors.New("custom OAuth endpoints must use HTTPS")
		}
	}
	return nil
}

func customProviderDefinition(provider *EnvOAuthCustomProvider) *EnvOAuthProviderDefinition {
	if provider == nil {
		return nil
	}
	return new(provider.EnvOAuthProviderDefinition)
}

func customProviderSecret(provider *EnvOAuthCustomProvider) string {
	if provider == nil {
		return ""
	}
	return provider.ClientSecret
}

func OpenEnvOAuthCustomProvider(ctx context.Context, service *secrets.Service, credential db.VaultCredential) (*EnvOAuthCustomProvider, error) {
	public, err := decodeEnvOAuthCredentialAuth(credential.Auth)
	if err != nil {
		return nil, err
	}
	if public.CustomProvider == nil {
		return nil, errors.New("credential has no custom OAuth provider")
	}
	plaintext, err := openCredentialSecret(ctx, service, credential)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	secret, err := decodeEnvOAuthCredentialSecret(plaintext)
	if err != nil {
		return nil, err
	}
	return &EnvOAuthCustomProvider{EnvOAuthProviderDefinition: *public.CustomProvider, ClientSecret: secret.ClientSecret}, nil
}

func ResolveEnvOAuthCredentialProvider(ctx context.Context, service *secrets.Service, credential db.VaultCredential, providers EnvOAuthProviderResolver) (EnvOAuthProviderSnapshot, error) {
	public, err := decodeEnvOAuthCredentialAuth(credential.Auth)
	if err != nil {
		return EnvOAuthProviderSnapshot{}, err
	}
	if public.CustomProvider == nil {
		if providers == nil {
			return EnvOAuthProviderSnapshot{}, ErrEnvOAuthProviderUnavailable
		}
		return providers.ResolveEnvOAuthProvider(ctx, public.Provider)
	}
	custom, err := OpenEnvOAuthCustomProvider(ctx, service, credential)
	if err != nil {
		return EnvOAuthProviderSnapshot{}, err
	}
	var hosts []string
	if public.Networking.AllowedHosts != nil {
		hosts = slices.Clone(*public.Networking.AllowedHosts)
	}
	return EnvOAuthProviderSnapshot{
		ProviderKey: EnvOAuthProviderInline, Version: credential.SecretVersion, Enabled: true,
		Configured: custom.ClientID != "" && custom.ClientSecret != "", Name: custom.Name,
		AuthorizeEndpoint: custom.AuthorizeEndpoint, TokenEndpoint: custom.TokenEndpoint, Scope: custom.Scope,
		ClientID: custom.ClientID, ClientSecret: custom.ClientSecret,
		TokenEndpointAuthMethod: cmp.Or(custom.TokenEndpointAuthMethod, "client_secret_post"), TokenEndpointEncoding: "form",
		DefaultSecretName: credential.CredentialKey, DefaultAllowedHosts: hosts,
	}, nil
}
