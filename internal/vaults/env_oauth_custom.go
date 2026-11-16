package vaults

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type EnvOAuthProviderDefinition struct {
	Name              string `json:"name"`
	AuthorizeEndpoint string `json:"authorize_endpoint"`
	TokenEndpoint     string `json:"token_endpoint"`
	Scope             string `json:"scope"`
	ClientID          string `json:"client_id,omitempty"`
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
	if provider.Name == "" || utf8.RuneCountInString(provider.Name) > 200 {
		return errors.New("custom_provider.name is required and must not exceed 200 characters")
	}
	for _, endpoint := range []string{provider.AuthorizeEndpoint, provider.TokenEndpoint} {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1"))) {
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
