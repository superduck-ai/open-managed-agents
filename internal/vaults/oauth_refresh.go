package vaults

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const maxOAuthRefreshCASAttempts = 3

var errMCPOAuthRefreshUnavailable = errors.New("mcp_oauth refresh unavailable")

type mcpOAuthPublicAuth struct {
	Type         string          `json:"type"`
	MCPServerURL string          `json:"mcp_server_url"`
	ExpiresAt    string          `json:"expires_at"`
	Refresh      json.RawMessage `json:"refresh"`
}

type mcpOAuthPublicRefresh struct {
	TokenEndpoint     string          `json:"token_endpoint"`
	ClientID          string          `json:"client_id"`
	Scope             string          `json:"scope"`
	Resource          string          `json:"resource"`
	TokenEndpointAuth json.RawMessage `json:"token_endpoint_auth"`
}

type mcpOAuthSecretPayload struct {
	Type         string          `json:"type"`
	AccessToken  string          `json:"access_token"`
	Refresh      json.RawMessage `json:"refresh"`
}

type mcpOAuthSecretRefresh struct {
	RefreshToken      string          `json:"refresh_token"`
	TokenEndpointAuth json.RawMessage `json:"token_endpoint_auth"`
}

type mcpOAuthTokenEndpointAuth struct {
	Type         string `json:"type"`
	ClientSecret string `json:"client_secret"`
}

type mcpOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    any    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func accessTokenExpired(expiresAt string, now time.Time) (bool, error) {
	expiresAt = strings.TrimSpace(expiresAt)
	if expiresAt == "" {
		return false, nil
	}
	parsed, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, expiresAt)
		if err != nil {
			return false, fmt.Errorf("parse expires_at: %w", err)
		}
	}
	return !now.Before(parsed.UTC()), nil
}

func parseMCPOAuthPublicAuth(raw json.RawMessage) (mcpOAuthPublicAuth, error) {
	var auth mcpOAuthPublicAuth
	if len(raw) == 0 {
		return auth, errors.New("empty mcp_oauth auth")
	}
	if err := json.Unmarshal(raw, &auth); err != nil {
		return auth, err
	}
	return auth, nil
}

func parseMCPOAuthSecret(raw json.RawMessage) (mcpOAuthSecretPayload, error) {
	var secret mcpOAuthSecretPayload
	if len(raw) == 0 {
		return secret, errors.New("empty mcp_oauth secret")
	}
	if err := json.Unmarshal(raw, &secret); err != nil {
		return secret, err
	}
	return secret, nil
}

func hasMCPOAuthRefreshMaterial(publicAuth mcpOAuthPublicAuth, secret mcpOAuthSecretPayload) bool {
	if len(publicAuth.Refresh) == 0 || isJSONNull(publicAuth.Refresh) {
		return false
	}
	if len(secret.Refresh) == 0 || isJSONNull(secret.Refresh) {
		return false
	}
	var publicRefresh mcpOAuthPublicRefresh
	var secretRefresh mcpOAuthSecretRefresh
	if err := json.Unmarshal(publicAuth.Refresh, &publicRefresh); err != nil {
		return false
	}
	if err := json.Unmarshal(secret.Refresh, &secretRefresh); err != nil {
		return false
	}
	return strings.TrimSpace(publicRefresh.TokenEndpoint) != "" &&
		strings.TrimSpace(publicRefresh.ClientID) != "" &&
		strings.TrimSpace(secretRefresh.RefreshToken) != ""
}

func (i *Injector) refreshMCPOAuthCredential(
	ctx context.Context,
	credential *db.VaultCredential,
	now time.Time,
	force bool,
) (string, *db.VaultCredential, error) {
	if i == nil || i.db == nil {
		return "", nil, errMCPOAuthRefreshUnavailable
	}
	current := *credential
	for attempt := 0; attempt < maxOAuthRefreshCASAttempts; attempt++ {
		plaintext, err := openCredentialSecret(ctx, i.secretSvc, current)
		if err != nil {
			return "", nil, err
		}
		publicAuth, err := parseMCPOAuthPublicAuth(current.Auth)
		if err != nil {
			clear(plaintext)
			return "", nil, err
		}
		secret, err := parseMCPOAuthSecret(plaintext)
		if err != nil {
			clear(plaintext)
			return "", nil, err
		}
		expired, err := accessTokenExpired(publicAuth.ExpiresAt, now)
		if err != nil {
			clear(plaintext)
			return "", nil, err
		}
		if !force && !expired && strings.TrimSpace(secret.AccessToken) != "" {
			token := strings.TrimSpace(secret.AccessToken)
			clear(plaintext)
			return token, &current, nil
		}
		if !hasMCPOAuthRefreshMaterial(publicAuth, secret) {
			clear(plaintext)
			return "", nil, errMCPOAuthRefreshUnavailable
		}
		token, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(ctx, i.client(), publicAuth, secret, now)
		clear(plaintext)
		if err != nil {
			return "", nil, err
		}
		updated := current
		updated.Auth = nextAuth
		updated.SecretPayload = nextSecret
		updated.UpdatedAt = now.UTC()
		if err := SealCredentialSecret(ctx, i.secretSvc, &updated); err != nil {
			return "", nil, err
		}
		saved, err := i.db.UpdateVaultCredential(ctx, updated.WorkspaceUUID, updated.VaultExternalID, updated.ExternalID, updated)
		if err == nil {
			return token, &saved, nil
		}
		if !errors.Is(err, db.ErrVersionConflict) {
			return "", nil, err
		}
		reloaded, getErr := i.db.GetVaultCredential(ctx, current.WorkspaceUUID, current.VaultExternalID, current.ExternalID)
		if getErr != nil {
			return "", nil, getErr
		}
		current = reloaded
		force = true
	}
	return "", nil, errMCPOAuthRefreshUnavailable
}

func exchangeMCPOAuthRefresh(
	ctx context.Context,
	client *http.Client,
	publicAuth mcpOAuthPublicAuth,
	secret mcpOAuthSecretPayload,
	now time.Time,
) (string, json.RawMessage, json.RawMessage, error) {
	var publicRefresh mcpOAuthPublicRefresh
	var secretRefresh mcpOAuthSecretRefresh
	if err := json.Unmarshal(publicAuth.Refresh, &publicRefresh); err != nil {
		return "", nil, nil, err
	}
	if err := json.Unmarshal(secret.Refresh, &secretRefresh); err != nil {
		return "", nil, nil, err
	}
	authMethod := mcpOAuthTokenEndpointAuth{Type: "none"}
	if len(secretRefresh.TokenEndpointAuth) > 0 && !isJSONNull(secretRefresh.TokenEndpointAuth) {
		if err := json.Unmarshal(secretRefresh.TokenEndpointAuth, &authMethod); err != nil {
			return "", nil, nil, err
		}
	}
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", secretRefresh.RefreshToken)
	values.Set("client_id", publicRefresh.ClientID)
	if scope := strings.TrimSpace(publicRefresh.Scope); scope != "" {
		values.Set("scope", scope)
	}
	if resource := strings.TrimSpace(publicRefresh.Resource); resource != "" {
		values.Set("resource", resource)
	}
	method := strings.TrimSpace(authMethod.Type)
	switch method {
	case "", "none":
		method = "none"
	case "client_secret_basic":
		if strings.TrimSpace(authMethod.ClientSecret) == "" {
			return "", nil, nil, errors.New("client_secret_basic selected without client secret")
		}
	case "client_secret_post":
		if strings.TrimSpace(authMethod.ClientSecret) == "" {
			return "", nil, nil, errors.New("client_secret_post selected without client secret")
		}
		values.Set("client_secret", authMethod.ClientSecret)
	default:
		return "", nil, nil, fmt.Errorf("unsupported token auth method %q", method)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, publicRefresh.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if method == "client_secret_basic" {
		basic := url.QueryEscape(publicRefresh.ClientID) + ":" + url.QueryEscape(authMethod.ClientSecret)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(basic)))
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", nil, nil, err
	}
	var token mcpOAuthTokenResponse
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &token)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if token.Error != "" {
			return "", nil, nil, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, token.Error)
		}
		return "", nil, nil, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	accessToken := strings.TrimSpace(token.AccessToken)
	if accessToken == "" {
		return "", nil, nil, errors.New("token endpoint returned no access_token")
	}

	nextPublic := map[string]any{
		"type":           "mcp_oauth",
		"mcp_server_url": publicAuth.MCPServerURL,
		"refresh":        mustObjectFromRaw(publicAuth.Refresh),
	}
	if expiresIn := parseOAuthExpiresIn(token.ExpiresIn); expiresIn > 0 {
		nextPublic["expires_at"] = now.UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
	}
	if scope := strings.TrimSpace(token.Scope); scope != "" {
		refreshObj := mustObjectFromRaw(publicAuth.Refresh)
		refreshObj["scope"] = scope
		nextPublic["refresh"] = refreshObj
	}

	nextRefreshToken := strings.TrimSpace(token.RefreshToken)
	if nextRefreshToken == "" {
		nextRefreshToken = secretRefresh.RefreshToken
	}
	nextSecret := map[string]any{
		"type":         "mcp_oauth",
		"access_token": accessToken,
		"refresh": map[string]any{
			"refresh_token":       nextRefreshToken,
			"token_endpoint_auth": mustObjectFromRaw(secretRefresh.TokenEndpointAuth),
		},
	}
	publicJSON, err := json.Marshal(nextPublic)
	if err != nil {
		return "", nil, nil, err
	}
	secretJSON, err := json.Marshal(nextSecret)
	if err != nil {
		return "", nil, nil, err
	}
	return accessToken, append(json.RawMessage(nil), publicJSON...), append(json.RawMessage(nil), secretJSON...), nil
}

func mustObjectFromRaw(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || isJSONNull(raw) {
		return map[string]any{}
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return map[string]any{}
	}
	return obj
}

func parseOAuthExpiresIn(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}
