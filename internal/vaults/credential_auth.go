package vaults

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var credentialHostPattern = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9.-]+$`)

type credentialAuthState struct {
	AuthType      string
	Key           string
	PublicAuth    json.RawMessage
	SecretPayload json.RawMessage
}

type mcpOAuthCredentialCreateInput struct {
	MCPServerURL string                      `json:"mcp_server_url"`
	AccessToken  string                      `json:"access_token"`
	ExpiresAt    *string                     `json:"expires_at"`
	Refresh      *mcpOAuthRefreshCreateInput `json:"refresh"`
}

type mcpOAuthRefreshCreateInput struct {
	TokenEndpoint     string                  `json:"token_endpoint"`
	ClientID          string                  `json:"client_id"`
	TokenEndpointAuth *tokenEndpointAuthInput `json:"token_endpoint_auth"`
	Scope             *string                 `json:"scope"`
	Resource          *string                 `json:"resource"`
	RefreshToken      string                  `json:"refresh_token"`
}

type tokenEndpointAuthInput struct {
	Type         string `json:"type"`
	ClientSecret string `json:"client_secret"`
}

type staticBearerCredentialCreateInput struct {
	MCPServerURL string `json:"mcp_server_url"`
	Token        string `json:"token"`
}

type environmentVariableCredentialCreateInput struct {
	SecretName  string                     `json:"secret_name"`
	SecretValue string                     `json:"secret_value"`
	Networking  *credentialNetworkingInput `json:"networking"`
}

type credentialNetworkingInput struct {
	Type         string   `json:"type"`
	AllowedHosts []string `json:"allowed_hosts"`
}

type mcpOAuthCredentialUpdateInput struct {
	MCPServerURL *string         `json:"mcp_server_url"`
	AccessToken  *string         `json:"access_token"`
	ExpiresAt    *string         `json:"expires_at"`
	Refresh      json.RawMessage `json:"refresh"`
}

type mcpOAuthRefreshUpdateInput struct {
	TokenEndpoint     *string         `json:"token_endpoint"`
	ClientID          *string         `json:"client_id"`
	TokenEndpointAuth json.RawMessage `json:"token_endpoint_auth"`
	Scope             json.RawMessage `json:"scope"`
	Resource          *string         `json:"resource"`
	RefreshToken      *string         `json:"refresh_token"`
}

type staticBearerCredentialUpdateInput struct {
	MCPServerURL *string `json:"mcp_server_url"`
	Token        *string `json:"token"`
}

type environmentVariableCredentialUpdateInput struct {
	SecretName  *string         `json:"secret_name"`
	SecretValue *string         `json:"secret_value"`
	Networking  json.RawMessage `json:"networking"`
}

func normalizeCredentialAuthForCreate(raw json.RawMessage) (credentialAuthState, error) {
	authType, err := credentialAuthTypeFromInput(raw)
	if err != nil {
		return credentialAuthState{}, err
	}
	switch authType {
	case credentialAuthTypeMCPOAuth:
		var input mcpOAuthCredentialCreateInput
		if err := decodeCredentialAuthInput(raw, &input); err != nil {
			return credentialAuthState{}, err
		}
		return normalizeMCPOAuthForCreate(input)
	case credentialAuthTypeStaticBearer:
		var input staticBearerCredentialCreateInput
		if err := decodeCredentialAuthInput(raw, &input); err != nil {
			return credentialAuthState{}, err
		}
		return normalizeStaticBearerForCreate(input)
	case credentialAuthTypeEnvironmentVariable:
		var input environmentVariableCredentialCreateInput
		if err := decodeCredentialAuthInput(raw, &input); err != nil {
			return credentialAuthState{}, err
		}
		return normalizeEnvironmentVariableForCreate(input)
	default:
		return credentialAuthState{}, errors.New("auth.type must be mcp_oauth, static_bearer, or environment_variable")
	}
}

func normalizeMCPOAuthForCreate(input mcpOAuthCredentialCreateInput) (credentialAuthState, error) {
	serverURL, err := requireNonEmptyString(input.MCPServerURL, "auth.mcp_server_url")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
		return credentialAuthState{}, err
	}
	accessToken, err := requireNonEmptyString(input.AccessToken, "auth.access_token")
	if err != nil {
		return credentialAuthState{}, err
	}
	if input.ExpiresAt != nil {
		if _, err := requireNonEmptyString(*input.ExpiresAt, "auth.expires_at"); err != nil {
			return credentialAuthState{}, err
		}
		if err := validateRFC3339(*input.ExpiresAt, "auth.expires_at"); err != nil {
			return credentialAuthState{}, err
		}
	}
	publicAuth := &mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: serverURL,
		ExpiresAt:    input.ExpiresAt,
	}
	secretPayload := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: accessToken,
	}
	if input.Refresh != nil {
		publicRefresh, secretRefresh, err := normalizeMCPOAuthRefreshForCreate(*input.Refresh)
		if err != nil {
			return credentialAuthState{}, err
		}
		publicAuth.Refresh = &publicRefresh
		secretPayload.Refresh = &secretRefresh
	}
	return credentialAuthStateFromValues(credentialAuthTypeMCPOAuth, serverURL, publicAuth, secretPayload)
}

func normalizeMCPOAuthRefreshForCreate(input mcpOAuthRefreshCreateInput) (mcpOAuthRefresh, mcpOAuthRefreshSecret, error) {
	tokenEndpoint, err := requireNonEmptyString(input.TokenEndpoint, "auth.refresh.token_endpoint")
	if err != nil {
		return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
	}
	if err := validateHTTPURL(tokenEndpoint, "auth.refresh.token_endpoint"); err != nil {
		return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
	}
	clientID, err := requireNonEmptyString(input.ClientID, "auth.refresh.client_id")
	if err != nil {
		return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
	}
	refreshToken, err := requireNonEmptyString(input.RefreshToken, "auth.refresh.refresh_token")
	if err != nil {
		return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
	}
	if input.Scope != nil {
		if _, err := requireNonEmptyString(*input.Scope, "auth.refresh.scope"); err != nil {
			return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
		}
	}
	if input.Resource != nil {
		if _, err := requireNonEmptyString(*input.Resource, "auth.refresh.resource"); err != nil {
			return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
		}
	}
	publicTokenAuth, secretTokenAuth, err := normalizeTokenEndpointAuth(input.TokenEndpointAuth)
	if err != nil {
		return mcpOAuthRefresh{}, mcpOAuthRefreshSecret{}, err
	}
	return mcpOAuthRefresh{
		TokenEndpoint:     tokenEndpoint,
		ClientID:          clientID,
		TokenEndpointAuth: publicTokenAuth,
		Scope:             input.Scope,
		Resource:          input.Resource,
	}, mcpOAuthRefreshSecret{
		RefreshToken:      refreshToken,
		TokenEndpointAuth: &secretTokenAuth,
	}, nil
}

func normalizeTokenEndpointAuth(input *tokenEndpointAuthInput) (tokenEndpointAuth, tokenEndpointAuthSecret, error) {
	if input == nil {
		return tokenEndpointAuth{Type: "none"}, tokenEndpointAuthSecret{Type: "none"}, nil
	}
	authType, err := requireNonEmptyString(input.Type, "auth.refresh.token_endpoint_auth.type")
	if err != nil {
		return tokenEndpointAuth{}, tokenEndpointAuthSecret{}, err
	}
	switch authType {
	case "none":
		return tokenEndpointAuth{Type: "none"}, tokenEndpointAuthSecret{Type: "none"}, nil
	case "client_secret_basic", "client_secret_post":
		clientSecret, err := requireNonEmptyString(input.ClientSecret, "auth.refresh.token_endpoint_auth.client_secret")
		if err != nil {
			return tokenEndpointAuth{}, tokenEndpointAuthSecret{}, err
		}
		return tokenEndpointAuth{Type: authType}, tokenEndpointAuthSecret{Type: authType, ClientSecret: clientSecret}, nil
	default:
		return tokenEndpointAuth{}, tokenEndpointAuthSecret{}, errors.New("auth.refresh.token_endpoint_auth.type must be none, client_secret_basic, or client_secret_post")
	}
}

func normalizeStaticBearerForCreate(input staticBearerCredentialCreateInput) (credentialAuthState, error) {
	serverURL, err := requireNonEmptyString(input.MCPServerURL, "auth.mcp_server_url")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
		return credentialAuthState{}, err
	}
	token, err := requireNonEmptyString(input.Token, "auth.token")
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := &staticBearerCredentialAuth{Type: credentialAuthTypeStaticBearer, MCPServerURL: serverURL}
	secretPayload := staticBearerCredentialSecret{Type: credentialAuthTypeStaticBearer, Token: token}
	return credentialAuthStateFromValues(credentialAuthTypeStaticBearer, serverURL, publicAuth, secretPayload)
}

func normalizeEnvironmentVariableForCreate(input environmentVariableCredentialCreateInput) (credentialAuthState, error) {
	secretName, err := requireNonEmptyString(input.SecretName, "auth.secret_name")
	if err != nil {
		return credentialAuthState{}, err
	}
	if err := validateSecretName(secretName); err != nil {
		return credentialAuthState{}, err
	}
	secretValue, err := requireNonEmptyString(input.SecretValue, "auth.secret_value")
	if err != nil {
		return credentialAuthState{}, err
	}
	networking, err := normalizeCredentialNetworking(input.Networking)
	if err != nil {
		return credentialAuthState{}, err
	}
	publicAuth := &environmentVariableCredentialAuth{
		Type:       credentialAuthTypeEnvironmentVariable,
		SecretName: secretName,
		Networking: networking,
	}
	secretPayload := environmentVariableCredentialSecret{Type: credentialAuthTypeEnvironmentVariable, SecretValue: secretValue}
	return credentialAuthStateFromValues(credentialAuthTypeEnvironmentVariable, secretName, publicAuth, secretPayload)
}

func normalizeCredentialAuthForUpdate(current db.VaultCredential, currentSecret []byte, raw json.RawMessage) (credentialAuthState, error) {
	authType, err := credentialAuthTypeFromInput(raw)
	if err != nil {
		return credentialAuthState{}, err
	}
	if string(authType) != current.AuthType {
		return credentialAuthState{}, errors.New("auth.type cannot be changed")
	}
	stored, err := decodeCredentialAuth(current.Auth)
	if err != nil {
		return credentialAuthState{}, fmt.Errorf("decode stored credential auth: %w", err)
	}

	switch authType {
	case credentialAuthTypeMCPOAuth:
		var input mcpOAuthCredentialUpdateInput
		if err := decodeCredentialAuthInput(raw, &input); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth, ok := stored.value.(*mcpOAuthCredentialAuth)
		if !ok {
			return credentialAuthState{}, errors.New("stored credential auth type is invalid")
		}
		return normalizeMCPOAuthForUpdate(current, currentSecret, input, publicAuth)
	case credentialAuthTypeStaticBearer:
		var input staticBearerCredentialUpdateInput
		if err := decodeCredentialAuthInput(raw, &input); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth, ok := stored.value.(*staticBearerCredentialAuth)
		if !ok {
			return credentialAuthState{}, errors.New("stored credential auth type is invalid")
		}
		return normalizeStaticBearerForUpdate(current, currentSecret, input, publicAuth)
	case credentialAuthTypeEnvironmentVariable:
		var input environmentVariableCredentialUpdateInput
		if err := decodeCredentialAuthInput(raw, &input); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth, ok := stored.value.(*environmentVariableCredentialAuth)
		if !ok {
			return credentialAuthState{}, errors.New("stored credential auth type is invalid")
		}
		return normalizeEnvironmentVariableForUpdate(current, currentSecret, input, publicAuth)
	default:
		return credentialAuthState{}, errors.New("stored credential auth type is invalid")
	}
}

func normalizeMCPOAuthForUpdate(current db.VaultCredential, currentSecret []byte, input mcpOAuthCredentialUpdateInput, publicAuth *mcpOAuthCredentialAuth) (credentialAuthState, error) {
	secretPayload := mcpOAuthCredentialSecret{Type: credentialAuthTypeMCPOAuth}
	if len(currentSecret) != 0 {
		var err error
		secretPayload, err = decodeMCPOAuthCredentialSecret(currentSecret)
		if err != nil {
			return credentialAuthState{}, err
		}
	}
	if input.MCPServerURL != nil {
		return credentialAuthState{}, errors.New("auth.mcp_server_url is immutable")
	}
	if input.AccessToken != nil {
		accessToken, err := requireNonEmptyString(*input.AccessToken, "auth.access_token")
		if err != nil {
			return credentialAuthState{}, err
		}
		secretPayload.AccessToken = accessToken
	}
	if input.ExpiresAt != nil {
		expiresAt, err := requireNonEmptyString(*input.ExpiresAt, "auth.expires_at")
		if err != nil {
			return credentialAuthState{}, err
		}
		if err := validateRFC3339(expiresAt, "auth.expires_at"); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth.ExpiresAt = &expiresAt
	}
	if len(input.Refresh) != 0 {
		if isJSONNull(input.Refresh) {
			publicAuth.Refresh = nil
			secretPayload.Refresh = nil
		} else if err := patchMCPOAuthRefreshForUpdate(publicAuth, &secretPayload, input.Refresh); err != nil {
			return credentialAuthState{}, err
		}
	}
	if secretPayload.AccessToken == "" {
		return credentialAuthState{}, ErrMissingSecretEnvelope
	}
	if publicAuth.Refresh != nil {
		if secretPayload.Refresh == nil || secretPayload.Refresh.RefreshToken == "" {
			return credentialAuthState{}, ErrMissingSecretEnvelope
		}
		tokenAuthType := publicAuth.Refresh.TokenEndpointAuth.Type
		if (tokenAuthType == "client_secret_basic" || tokenAuthType == "client_secret_post") &&
			(secretPayload.Refresh.TokenEndpointAuth == nil || secretPayload.Refresh.TokenEndpointAuth.ClientSecret == "") {
			return credentialAuthState{}, ErrMissingSecretEnvelope
		}
	}
	return credentialAuthStateFromValues(credentialAuthTypeMCPOAuth, current.CredentialKey, publicAuth, secretPayload)
}

func normalizeStaticBearerForUpdate(current db.VaultCredential, currentSecret []byte, input staticBearerCredentialUpdateInput, publicAuth *staticBearerCredentialAuth) (credentialAuthState, error) {
	secretPayload := staticBearerCredentialSecret{Type: credentialAuthTypeStaticBearer}
	if len(currentSecret) != 0 {
		var err error
		secretPayload, err = decodeStaticBearerCredentialSecret(currentSecret)
		if err != nil {
			return credentialAuthState{}, err
		}
	}
	key := current.CredentialKey
	if input.MCPServerURL != nil {
		serverURL, err := requireNonEmptyString(*input.MCPServerURL, "auth.mcp_server_url")
		if err != nil {
			return credentialAuthState{}, err
		}
		if err := validateHTTPURL(serverURL, "auth.mcp_server_url"); err != nil {
			return credentialAuthState{}, err
		}
		publicAuth.MCPServerURL = serverURL
		key = serverURL
	}
	if input.Token != nil {
		token, err := requireNonEmptyString(*input.Token, "auth.token")
		if err != nil {
			return credentialAuthState{}, err
		}
		secretPayload.Token = token
	}
	if secretPayload.Token == "" {
		return credentialAuthState{}, ErrMissingSecretEnvelope
	}
	return credentialAuthStateFromValues(credentialAuthTypeStaticBearer, key, publicAuth, secretPayload)
}

func normalizeEnvironmentVariableForUpdate(current db.VaultCredential, currentSecret []byte, input environmentVariableCredentialUpdateInput, publicAuth *environmentVariableCredentialAuth) (credentialAuthState, error) {
	secretPayload := environmentVariableCredentialSecret{Type: credentialAuthTypeEnvironmentVariable}
	if len(currentSecret) != 0 {
		var err error
		secretPayload, err = decodeEnvironmentVariableCredentialSecret(currentSecret)
		if err != nil {
			return credentialAuthState{}, err
		}
	}
	if input.SecretName != nil {
		return credentialAuthState{}, errors.New("auth.secret_name is immutable")
	}
	if input.SecretValue != nil {
		secretValue, err := requireNonEmptyString(*input.SecretValue, "auth.secret_value")
		if err != nil {
			return credentialAuthState{}, err
		}
		secretPayload.SecretValue = secretValue
	}
	if len(input.Networking) != 0 {
		var networkingInput *credentialNetworkingInput
		if err := json.Unmarshal(input.Networking, &networkingInput); err != nil {
			return credentialAuthState{}, errors.New("auth.networking must be an object")
		}
		networking, err := normalizeCredentialNetworking(networkingInput)
		if err != nil {
			return credentialAuthState{}, err
		}
		publicAuth.Networking = networking
	}
	if secretPayload.SecretValue == "" {
		return credentialAuthState{}, ErrMissingSecretEnvelope
	}
	return credentialAuthStateFromValues(credentialAuthTypeEnvironmentVariable, current.CredentialKey, publicAuth, secretPayload)
}

func patchMCPOAuthRefreshForUpdate(publicAuth *mcpOAuthCredentialAuth, secretPayload *mcpOAuthCredentialSecret, raw json.RawMessage) error {
	var input mcpOAuthRefreshUpdateInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return errors.New("auth.refresh must be an object")
	}
	if input.TokenEndpoint != nil {
		return errors.New("auth.refresh.token_endpoint is immutable")
	}
	if input.ClientID != nil {
		return errors.New("auth.refresh.client_id is immutable")
	}
	if input.Resource != nil {
		return errors.New("auth.refresh.resource is immutable")
	}
	if publicAuth.Refresh == nil {
		return errors.New("auth.refresh cannot be added after creation")
	}
	if secretPayload.Refresh == nil {
		secretPayload.Refresh = &mcpOAuthRefreshSecret{}
	}
	if input.RefreshToken != nil {
		refreshToken, err := requireNonEmptyString(*input.RefreshToken, "auth.refresh.refresh_token")
		if err != nil {
			return err
		}
		secretPayload.Refresh.RefreshToken = refreshToken
	}
	if len(input.Scope) != 0 {
		if isJSONNull(input.Scope) {
			publicAuth.Refresh.Scope = nil
		} else {
			var scope string
			if err := json.Unmarshal(input.Scope, &scope); err != nil {
				return errors.New("auth.refresh.scope must be a string")
			}
			if _, err := requireNonEmptyString(scope, "auth.refresh.scope"); err != nil {
				return err
			}
			publicAuth.Refresh.Scope = &scope
		}
	}
	if len(input.TokenEndpointAuth) != 0 {
		var tokenAuthInput *tokenEndpointAuthInput
		if err := json.Unmarshal(input.TokenEndpointAuth, &tokenAuthInput); err != nil {
			return errors.New("auth.refresh.token_endpoint_auth must be an object")
		}
		publicTokenAuth, secretTokenAuth, err := normalizeTokenEndpointAuth(tokenAuthInput)
		if err != nil {
			return err
		}
		publicAuth.Refresh.TokenEndpointAuth = publicTokenAuth
		secretPayload.Refresh.TokenEndpointAuth = &secretTokenAuth
	}
	return nil
}

func normalizeCredentialNetworking(input *credentialNetworkingInput) (credentialAuthNetworking, error) {
	if input == nil || input.Type == "" || input.Type == "unrestricted" {
		return credentialAuthNetworking{Type: "unrestricted"}, nil
	}
	if input.Type != "limited" {
		return credentialAuthNetworking{}, errors.New("auth.networking.type must be unrestricted or limited")
	}
	if len(input.AllowedHosts) > 16 {
		return credentialAuthNetworking{}, errors.New("auth.networking.allowed_hosts must contain at most 16 hosts")
	}
	for _, host := range input.AllowedHosts {
		if _, err := requireNonEmptyString(host, "auth.networking.allowed_hosts entry"); err != nil {
			return credentialAuthNetworking{}, err
		}
		if err := validateCredentialHost(host); err != nil {
			return credentialAuthNetworking{}, err
		}
	}
	hosts := input.AllowedHosts
	return credentialAuthNetworking{Type: "limited", AllowedHosts: &hosts}, nil
}

func credentialAuthTypeFromInput(raw json.RawMessage) (credentialAuthType, error) {
	var input struct {
		Type credentialAuthType `json:"type"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return "", errors.New("auth must be an object with a string type")
	}
	if input.Type == "" {
		return "", errors.New("auth.type is required")
	}
	return input.Type, nil
}

func decodeCredentialAuthInput(raw json.RawMessage, input any) error {
	if err := json.Unmarshal(raw, input); err != nil {
		return fmt.Errorf("auth has invalid field types: %w", err)
	}
	return nil
}

func credentialAuthStateFromValues(authType credentialAuthType, key string, publicAuth credentialAuthVariant, secretPayload credentialSecretVariant) (credentialAuthState, error) {
	publicRaw, err := json.Marshal(credentialAuth{value: publicAuth})
	if err != nil {
		return credentialAuthState{}, err
	}
	secretRaw, err := json.Marshal(secretPayload)
	if err != nil {
		return credentialAuthState{}, err
	}
	return credentialAuthState{
		AuthType:      string(authType),
		Key:           key,
		PublicAuth:    publicRaw,
		SecretPayload: secretRaw,
	}, nil
}

func validateHTTPURL(value, name string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid URL", name)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", name)
	}
	return nil
}

func validateRFC3339(value, name string) error {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be RFC3339", name)
	}
	return nil
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
