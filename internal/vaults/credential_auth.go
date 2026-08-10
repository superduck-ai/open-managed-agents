package vaults

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var credentialHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9.-]+$`)

type credentialAuthState struct {
	AuthType      string
	Key           string
	PublicAuth    json.RawMessage
	SecretPayload json.RawMessage
}

func normalizeCredentialAuthForCreate(raw json.RawMessage) (credentialAuthState, error) {
	fields, err := objectFromRaw(raw, "auth")
	if err != nil {
		return credentialAuthState{}, err
	}
	authType, err := requiredString(fields, "type", "auth.type")
	if err != nil {
		return credentialAuthState{}, err
	}
	switch authType {
	case "mcp_oauth":
		return normalizeMCPOAuthForCreate(fields)
	case "static_bearer":
		return normalizeStaticBearerForCreate(fields)
	case "environment_variable":
		return normalizeEnvironmentVariableForCreate(fields)
	default:
		return credentialAuthState{}, errors.New("auth.type must be mcp_oauth, static_bearer, or environment_variable")
	}
}

func normalizeMCPOAuthForCreate(fields map[string]json.RawMessage) (credentialAuthState, error) {
	serverURL, err := requiredString(fields, "mcp_server_url", "auth.mcp_server_url")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
		return credentialAuthState{}, err
	}
	accessToken, err := requiredString(fields, "access_token", "auth.access_token")
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := map[string]any{
		"type":           "mcp_oauth",
		"mcp_server_url": serverURL,
	}
	secretPayload := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: accessToken,
	}
	if expiresAt, ok, err := optionalString(fields, "expires_at", "auth.expires_at"); err != nil {
		return credentialAuthState{}, err
	} else if ok {
		if err := validateRFC3339(expiresAt, "auth.expires_at"); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["expires_at"] = expiresAt
	}
	if rawRefresh, ok := fields["refresh"]; ok && !isJSONNull(rawRefresh) {
		publicRefresh, secretRefresh, err := normalizeMCPOAuthRefreshForCreate(rawRefresh)
		if err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["refresh"] = publicRefresh
		secretPayload.Refresh = &secretRefresh
	}
	return credentialAuthStateFromValues("mcp_oauth", serverURL, publicAuth, secretPayload)
}

func normalizeMCPOAuthRefreshForCreate(raw json.RawMessage) (map[string]any, mcpOAuthRefreshSecret, error) {
	fields, err := objectFromRaw(raw, "auth.refresh")
	if err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	}
	tokenEndpoint, err := requiredString(fields, "token_endpoint", "auth.refresh.token_endpoint")
	if err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	}
	if err := validateHTTPURL(tokenEndpoint, "auth.refresh.token_endpoint"); err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	}
	clientID, err := requiredString(fields, "client_id", "auth.refresh.client_id")
	if err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	}
	refreshToken, err := requiredString(fields, "refresh_token", "auth.refresh.refresh_token")
	if err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	}
	publicTokenAuth, secretTokenAuth, err := normalizeTokenEndpointAuth(fields["token_endpoint_auth"])
	if err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	}
	publicRefresh := map[string]any{
		"token_endpoint":      tokenEndpoint,
		"client_id":           clientID,
		"token_endpoint_auth": publicTokenAuth,
	}
	secretRefresh := mcpOAuthRefreshSecret{
		RefreshToken:      refreshToken,
		TokenEndpointAuth: &secretTokenAuth,
	}
	if value, ok, err := optionalString(fields, "scope", "auth.refresh.scope"); err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	} else if ok {
		publicRefresh["scope"] = value
	}
	if value, ok, err := optionalString(fields, "resource", "auth.refresh.resource"); err != nil {
		return nil, mcpOAuthRefreshSecret{}, err
	} else if ok {
		publicRefresh["resource"] = value
	}
	return publicRefresh, secretRefresh, nil
}

func normalizeTokenEndpointAuth(raw json.RawMessage) (map[string]any, tokenEndpointAuthSecret, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{"type": "none"}, tokenEndpointAuthSecret{Type: "none"}, nil
	}
	fields, err := objectFromRaw(raw, "auth.refresh.token_endpoint_auth")
	if err != nil {
		return nil, tokenEndpointAuthSecret{}, err
	}
	authType, err := requiredString(fields, "type", "auth.refresh.token_endpoint_auth.type")
	if err != nil {
		return nil, tokenEndpointAuthSecret{}, err
	}
	switch authType {
	case "none":
		return map[string]any{"type": "none"}, tokenEndpointAuthSecret{Type: "none"}, nil
	case "client_secret_basic", "client_secret_post":
		clientSecret, err := requiredString(fields, "client_secret", "auth.refresh.token_endpoint_auth.client_secret")
		if err != nil {
			return nil, tokenEndpointAuthSecret{}, err
		}
		return map[string]any{"type": authType}, tokenEndpointAuthSecret{Type: authType, ClientSecret: clientSecret}, nil
	default:
		return nil, tokenEndpointAuthSecret{}, errors.New("auth.refresh.token_endpoint_auth.type must be none, client_secret_basic, or client_secret_post")
	}
}

func normalizeStaticBearerForCreate(fields map[string]json.RawMessage) (credentialAuthState, error) {
	serverURL, err := requiredString(fields, "mcp_server_url", "auth.mcp_server_url")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
		return credentialAuthState{}, err
	}
	token, err := requiredString(fields, "token", "auth.token")
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := map[string]any{"type": "static_bearer", "mcp_server_url": serverURL}
	secretPayload := staticBearerCredentialSecret{Type: credentialAuthTypeStaticBearer, Token: token}
	return credentialAuthStateFromValues("static_bearer", serverURL, publicAuth, secretPayload)
}

func normalizeEnvironmentVariableForCreate(fields map[string]json.RawMessage) (credentialAuthState, error) {
	secretName, err := requiredString(fields, "secret_name", "auth.secret_name")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateSecretName(secretName); err != nil {
		return credentialAuthState{}, err
	}
	secretValue, err := requiredString(fields, "secret_value", "auth.secret_value")
	if err != nil {
		return credentialAuthState{}, err
	}
	networking, err := normalizeCredentialNetworking(fields["networking"])
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := map[string]any{
		"type":        "environment_variable",
		"secret_name": secretName,
		"networking":  networking,
	}
	secretPayload := environmentVariableCredentialSecret{Type: credentialAuthTypeEnvironmentVariable, SecretValue: secretValue}
	return credentialAuthStateFromValues("environment_variable", secretName, publicAuth, secretPayload)
}

func normalizeCredentialAuthForUpdate(current db.VaultCredential, currentSecret []byte, raw json.RawMessage) (credentialAuthState, error) {
	fields, err := objectFromRaw(raw, "auth")
	if err != nil {
		return credentialAuthState{}, err
	}
	authType, err := requiredString(fields, "type", "auth.type")
	if err != nil {
		return credentialAuthState{}, err
	}
	if authType != current.AuthType {
		return credentialAuthState{}, errors.New("auth.type cannot be changed")
	}
	publicAuth := rawObjectMap(current.Auth)
	publicAuth["type"] = current.AuthType

	switch current.AuthType {
	case "mcp_oauth":
		return normalizeMCPOAuthForUpdate(current, currentSecret, fields, publicAuth)
	case "static_bearer":
		return normalizeStaticBearerForUpdate(current, currentSecret, fields, publicAuth)
	case "environment_variable":
		return normalizeEnvironmentVariableForUpdate(current, currentSecret, fields, publicAuth)
	default:
		return credentialAuthState{}, errors.New("stored credential auth type is invalid")
	}
}

func normalizeMCPOAuthForUpdate(current db.VaultCredential, currentSecret []byte, fields map[string]json.RawMessage, publicAuth map[string]any) (credentialAuthState, error) {
	secretPayload := mcpOAuthCredentialSecret{Type: credentialAuthTypeMCPOAuth}
	if len(currentSecret) != 0 {
		var err error
		secretPayload, err = decodeMCPOAuthCredentialSecret(currentSecret)
		if err != nil {
			return credentialAuthState{}, err
		}
	}
	if _, ok := fields["mcp_server_url"]; ok {
		return credentialAuthState{}, errors.New("auth.mcp_server_url is immutable")
	}
	if rawAccessToken, ok := fields["access_token"]; ok {
		accessToken, err := rawString(rawAccessToken, "auth.access_token")
		if err != nil {
			return credentialAuthState{}, err
		}
		secretPayload.AccessToken = accessToken
	}
	if rawExpiresAt, ok := fields["expires_at"]; ok {
		expiresAt, err := rawString(rawExpiresAt, "auth.expires_at")
		if err != nil {
			return credentialAuthState{}, err
		}
		if err := validateRFC3339(expiresAt, "auth.expires_at"); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["expires_at"] = expiresAt
	}
	if rawRefresh, ok := fields["refresh"]; ok {
		if isJSONNull(rawRefresh) {
			delete(publicAuth, "refresh")
			secretPayload.Refresh = nil
		} else if err := patchMCPOAuthRefreshForUpdate(publicAuth, &secretPayload, rawRefresh); err != nil {
			return credentialAuthState{}, err
		}
	}
	return credentialAuthStateFromValues(current.AuthType, current.CredentialKey, publicAuth, secretPayload)
}

func normalizeStaticBearerForUpdate(current db.VaultCredential, currentSecret []byte, fields map[string]json.RawMessage, publicAuth map[string]any) (credentialAuthState, error) {
	secretPayload := staticBearerCredentialSecret{Type: credentialAuthTypeStaticBearer}
	if len(currentSecret) != 0 {
		var err error
		secretPayload, err = decodeStaticBearerCredentialSecret(currentSecret)
		if err != nil {
			return credentialAuthState{}, err
		}
	}
	key := current.CredentialKey
	if rawServerURL, ok := fields["mcp_server_url"]; ok {
		serverURL, err := rawString(rawServerURL, "auth.mcp_server_url")
		if err != nil {
			return credentialAuthState{}, err
		}
		if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["mcp_server_url"] = serverURL
		key = serverURL
	}
	if rawToken, ok := fields["token"]; ok {
		token, err := rawString(rawToken, "auth.token")
		if err != nil {
			return credentialAuthState{}, err
		}
		secretPayload.Token = token
	}
	return credentialAuthStateFromValues(current.AuthType, key, publicAuth, secretPayload)
}

func normalizeEnvironmentVariableForUpdate(current db.VaultCredential, currentSecret []byte, fields map[string]json.RawMessage, publicAuth map[string]any) (credentialAuthState, error) {
	secretPayload := environmentVariableCredentialSecret{Type: credentialAuthTypeEnvironmentVariable}
	if len(currentSecret) != 0 {
		var err error
		secretPayload, err = decodeEnvironmentVariableCredentialSecret(currentSecret)
		if err != nil {
			return credentialAuthState{}, err
		}
	}
	if _, ok := fields["secret_name"]; ok {
		return credentialAuthState{}, errors.New("auth.secret_name is immutable")
	}
	if rawSecretValue, ok := fields["secret_value"]; ok {
		secretValue, err := rawString(rawSecretValue, "auth.secret_value")
		if err != nil {
			return credentialAuthState{}, err
		}
		secretPayload.SecretValue = secretValue
	}
	if rawNetworking, ok := fields["networking"]; ok {
		networking, err := normalizeCredentialNetworking(rawNetworking)
		if err != nil {
			return credentialAuthState{}, err
		}
		publicAuth["networking"] = networking
	}
	return credentialAuthStateFromValues(current.AuthType, current.CredentialKey, publicAuth, secretPayload)
}

// authUpdateProvidesSecretReplacement reports whether an auth update body
// carries enough secret material to reseal without opening an existing
// envelope (used to repair credentials that lost their envelope).
func authUpdateProvidesSecretReplacement(authType string, raw json.RawMessage) (bool, error) {
	fields, err := objectFromRaw(raw, "auth")
	if err != nil {
		return false, err
	}
	switch authType {
	case "static_bearer":
		_, ok := fields["token"]
		return ok, nil
	case "environment_variable":
		_, ok := fields["secret_value"]
		return ok, nil
	case "mcp_oauth":
		_, ok := fields["access_token"]
		return ok, nil
	default:
		return false, nil
	}
}

func patchMCPOAuthRefreshForUpdate(publicAuth map[string]any, secretPayload *mcpOAuthCredentialSecret, raw json.RawMessage) error {
	fields, err := objectFromRaw(raw, "auth.refresh")
	if err != nil {
		return err
	}
	if _, ok := fields["token_endpoint"]; ok {
		return errors.New("auth.refresh.token_endpoint is immutable")
	}
	if _, ok := fields["client_id"]; ok {
		return errors.New("auth.refresh.client_id is immutable")
	}
	if _, ok := fields["resource"]; ok {
		return errors.New("auth.refresh.resource is immutable")
	}
	publicRefresh := nestedMap(publicAuth, "refresh")
	if publicRefresh == nil {
		return errors.New("auth.refresh cannot be added after creation")
	}
	if secretPayload.Refresh == nil {
		secretPayload.Refresh = &mcpOAuthRefreshSecret{}
	}
	secretRefresh := secretPayload.Refresh
	if rawRefreshToken, ok := fields["refresh_token"]; ok {
		refreshToken, err := rawString(rawRefreshToken, "auth.refresh.refresh_token")
		if err != nil {
			return err
		}
		secretRefresh.RefreshToken = refreshToken
	}
	if rawScope, ok := fields["scope"]; ok {
		if isJSONNull(rawScope) {
			delete(publicRefresh, "scope")
		} else {
			scope, err := rawString(rawScope, "auth.refresh.scope")
			if err != nil {
				return err
			}
			publicRefresh["scope"] = scope
		}
	}
	if rawTokenAuth, ok := fields["token_endpoint_auth"]; ok {
		publicTokenAuth, secretTokenAuth, err := normalizeTokenEndpointAuth(rawTokenAuth)
		if err != nil {
			return err
		}
		publicRefresh["token_endpoint_auth"] = publicTokenAuth
		secretRefresh.TokenEndpointAuth = &secretTokenAuth
	}
	publicAuth["refresh"] = publicRefresh
	return nil
}

func normalizeCredentialNetworking(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{"type": "unrestricted"}, nil
	}
	fields, err := objectFromRaw(raw, "auth.networking")
	if err != nil {
		return nil, err
	}
	networkType := rawStringOrEmpty(fields["type"])
	if networkType == "" {
		networkType = "unrestricted"
	}
	switch networkType {
	case "unrestricted":
		return map[string]any{"type": "unrestricted"}, nil
	case "limited":
		hosts := []string{}
		if rawHosts, ok := fields["allowed_hosts"]; ok && !isJSONNull(rawHosts) {
			values, err := stringArray(rawHosts, "auth.networking.allowed_hosts")
			if err != nil {
				return nil, err
			}
			if len(values) > 16 {
				return nil, errors.New("auth.networking.allowed_hosts must contain at most 16 hosts")
			}
			for _, host := range values {
				if err := validateCredentialHost(host); err != nil {
					return nil, err
				}
			}
			hosts = values
		}
		return map[string]any{"type": "limited", "allowed_hosts": hosts}, nil
	default:
		return nil, errors.New("auth.networking.type must be unrestricted or limited")
	}
}

func credentialAuthStateFromValues(authType, key string, publicAuth map[string]any, secretPayload any) (credentialAuthState, error) {
	publicRaw, err := marshalRaw(publicAuth)
	if err != nil {
		return credentialAuthState{}, err
	}
	secretRaw, err := marshalRaw(secretPayload)
	if err != nil {
		return credentialAuthState{}, err
	}
	return credentialAuthState{
		AuthType:      authType,
		Key:           key,
		PublicAuth:    publicRaw,
		SecretPayload: secretRaw,
	}, nil
}

func validateSecretName(value string) error {
	if len(value) > 255 {
		return errors.New("auth.secret_name must be at most 255 characters")
	}
	return nil
}

func validateCredentialHost(host string) error {
	if strings.Contains(host, "://") || strings.Contains(host, "/") || strings.Contains(host, ":") || strings.Contains(host, "[") || strings.Contains(host, "]") {
		return errors.New("auth.networking.allowed_hosts entries must be hostnames without URL schemes")
	}
	if len(host) > 253 {
		return errors.New("auth.networking.allowed_hosts entries must be at most 253 characters")
	}
	if !credentialHostPattern.MatchString(host) {
		return errors.New("auth.networking.allowed_hosts entries must be valid hostnames")
	}
	return nil
}
