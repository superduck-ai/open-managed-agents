package vaults

import (
	"context"
	"errors"
	"strings"
)

const (
	EnvOAuthProviderBuiltinFeishu     = "builtin:feishu"
	EnvOAuthFeishuDefaultSecretName   = "LARKSUITE_CLI_USER_ACCESS_TOKEN"
	envOAuthProviderCustomPrefix      = "custom:"
	envOAuthTokenEndpointEncodingForm = "form"
	envOAuthTokenEndpointEncodingJSON = "json"
	envOAuthTokenEndpointAuthPost     = "client_secret_post"
	envOAuthTokenEndpointAuthBasic    = "client_secret_basic"
	envOAuthTokenEndpointAuthNone     = "none"
)

var errEnvOAuthProviderKeyInvalid = errors.New("env_oauth provider is invalid")

type EnvOAuthProviderSnapshot struct {
	ProviderKey             string
	Name                    string
	Builtin                 bool
	Configured              bool
	AuthorizeEndpoint       string
	TokenEndpoint           string
	ClientID                string
	ClientSecret            string
	Scope                   string
	TokenEndpointAuthMethod string
	TokenEndpointEncoding   string
	DefaultSecretName       string
	DefaultAllowedHosts     []string
}

type EnvOAuthProviderResolver interface {
	ResolveEnvOAuthProvider(ctx context.Context, providerKey string) (EnvOAuthProviderSnapshot, error)
}

func ParseEnvOAuthProviderKey(value string) (custom bool, id string, err error) {
	key := strings.TrimSpace(value)
	if key == "" {
		return false, "", errEnvOAuthProviderKeyInvalid
	}
	if key == EnvOAuthProviderBuiltinFeishu {
		return false, EnvOAuthProviderBuiltinFeishu, nil
	}
	if strings.HasPrefix(key, envOAuthProviderCustomPrefix) {
		id := strings.TrimSpace(strings.TrimPrefix(key, envOAuthProviderCustomPrefix))
		if id == "" || strings.ContainsAny(id, " \t\r\n") {
			return false, "", errEnvOAuthProviderKeyInvalid
		}
		return true, id, nil
	}
	return false, "", errEnvOAuthProviderKeyInvalid
}

func EnvOAuthCustomProviderKey(externalID string) string {
	return envOAuthProviderCustomPrefix + externalID
}

func IsBuiltinEnvOAuthProviderKey(value string) bool {
	return strings.TrimSpace(value) == EnvOAuthProviderBuiltinFeishu
}

func ParseEnvOAuthSecretName(value string) (string, error) {
	return parseSecretName(value)
}

func ValidateEnvOAuthNetworking(networkingType string, allowedHosts []string) error {
	_, err := normalizeCredentialNetworkingRequired(&credentialNetworkingInput{Type: networkingType, AllowedHosts: allowedHosts})
	return err
}
