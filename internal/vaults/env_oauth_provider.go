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

var (
	envOAuthFeishuAuthorizeEndpoint = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	envOAuthFeishuTokenEndpoint     = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	envOAuthFeishuScope             = "offline_access"
	envOAuthFeishuAllowedHosts      = []string{"open.feishu.cn", "accounts.feishu.cn"}
)

var errEnvOAuthProviderKeyInvalid = errors.New("env_oauth provider is invalid")

type EnvOAuthProviderSnapshot struct {
	ProviderKey             string
	Name                    string
	Builtin                 bool
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

func FeishuEnvOAuthProvider(clientID, clientSecret string) EnvOAuthProviderSnapshot {
	return EnvOAuthProviderSnapshot{
		ProviderKey:             EnvOAuthProviderBuiltinFeishu,
		Name:                    "Feishu",
		Builtin:                 true,
		AuthorizeEndpoint:       envOAuthFeishuAuthorizeEndpoint,
		TokenEndpoint:           envOAuthFeishuTokenEndpoint,
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		Scope:                   envOAuthFeishuScope,
		TokenEndpointAuthMethod: envOAuthTokenEndpointAuthPost,
		TokenEndpointEncoding:   envOAuthTokenEndpointEncodingJSON,
		DefaultSecretName:       EnvOAuthFeishuDefaultSecretName,
		DefaultAllowedHosts:     append([]string(nil), envOAuthFeishuAllowedHosts...),
	}
}

func ParseEnvOAuthSecretName(value string) (string, error) {
	return parseSecretName(value)
}

func ValidateEnvOAuthNetworking(networkingType string, allowedHosts []string) error {
	_, err := normalizeCredentialNetworkingRequired(&credentialNetworkingInput{Type: networkingType, AllowedHosts: allowedHosts})
	return err
}
