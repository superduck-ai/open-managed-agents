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

// mcpOAuthTokenResponse is the token-endpoint wire schema (external contract).
type mcpOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    any    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

func decodeMCPOAuthCredentialAuth(raw []byte) (*mcpOAuthCredentialAuth, error) {
	if len(raw) == 0 {
		return nil, emptyMCPOAuthAuth()
	}
	auth, err := decodeCredentialAuth(raw)
	if err != nil {
		return nil, err
	}
	value, ok := auth.value.(*mcpOAuthCredentialAuth)
	if !ok || value == nil {
		return nil, credentialAuthNotMCPOAuth()
	}
	if strings.TrimSpace(value.MCPServerURL) == "" {
		return nil, mcpOAuthServerURLRequired()
	}
	return value, nil
}

func accessTokenExpired(expiresAt *string, now time.Time) (bool, error) {
	if expiresAt == nil {
		return false, nil
	}
	value := strings.TrimSpace(*expiresAt)
	if value == "" {
		return false, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return false, fmt.Errorf("parse expires_at: %w", err)
		}
	}
	return !now.Before(parsed.UTC()), nil
}

func hasMCPOAuthRefreshMaterial(auth *mcpOAuthCredentialAuth, secret mcpOAuthCredentialSecret) bool {
	if auth == nil || auth.Refresh == nil || secret.Refresh == nil {
		return false
	}
	return strings.TrimSpace(auth.Refresh.TokenEndpoint) != "" &&
		strings.TrimSpace(auth.Refresh.ClientID) != "" &&
		strings.TrimSpace(secret.Refresh.RefreshToken) != ""
}

func (i *Injector) refreshMCPOAuthCredential(
	ctx context.Context,
	credential *db.VaultCredential,
	now time.Time,
	force bool,
) (string, *db.VaultCredential, error) {
	store := i.credentialStore()
	if i == nil || store == nil {
		return "", nil, errMCPOAuthRefreshUnavailable
	}
	lock := i.refreshLock(credential.ExternalID)
	lock.Lock()
	defer lock.Unlock()

	current := *credential
	// Re-read once under the per-credential lock: a concurrent winner may have
	// already persisted a usable token (one-time refresh_token safe).
	_ = reloadCredential(ctx, store, &current)
	for attempt := 0; attempt < maxOAuthRefreshCASAttempts; attempt++ {
		token, saved, retry, err := i.refreshMCPOAuthAttempt(ctx, store, &current, now, force)
		if err != nil {
			return "", nil, err
		}
		if retry {
			force = false
			continue
		}
		return token, saved, nil
	}
	return "", nil, errMCPOAuthRefreshUnavailable
}

// refreshMCPOAuthAttempt runs one open → maybe-exchange → CAS cycle.
// retry=true means reload already applied and the outer loop should try again.
func (i *Injector) refreshMCPOAuthAttempt(
	ctx context.Context,
	store credentialStore,
	current *db.VaultCredential,
	now time.Time,
	force bool,
) (token string, saved *db.VaultCredential, retry bool, err error) {
	plaintext, err := openCredentialSecret(ctx, i.secretSvc, *current)
	if err != nil {
		return "", nil, false, err
	}
	defer clear(plaintext)

	publicAuth, err := decodeMCPOAuthCredentialAuth(current.Auth)
	if err != nil {
		return "", nil, false, err
	}
	secret, err := decodeMCPOAuthCredentialSecret(plaintext)
	if err != nil {
		return "", nil, false, err
	}
	expired, err := accessTokenExpired(publicAuth.ExpiresAt, now)
	if err != nil {
		return "", nil, false, err
	}
	if !force && !expired && strings.TrimSpace(secret.AccessToken) != "" {
		return strings.TrimSpace(secret.AccessToken), current, false, nil
	}
	if !hasMCPOAuthRefreshMaterial(publicAuth, secret) {
		return "", nil, false, errMCPOAuthRefreshUnavailable
	}
	accessToken, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(ctx, i.client(), publicAuth, secret, now)
	if err != nil {
		// Exchange failed (e.g. invalid_grant after a concurrent winner
		// consumed the refresh_token). Reload once and reuse any usable
		// token instead of forcing another exchange.
		if reloadErr := reloadCredential(ctx, store, current); reloadErr != nil {
			return "", nil, false, err
		}
		return "", nil, true, nil
	}
	updated := *current
	updated.Auth = nextAuth
	updated.SecretPayload = nextSecret
	updated.UpdatedAt = now.UTC()
	if err := SealCredentialSecret(ctx, i.secretSvc, &updated); err != nil {
		return "", nil, false, err
	}
	row, err := store.UpdateVaultCredential(ctx, updated.WorkspaceUUID, updated.VaultExternalID, updated.ExternalID, updated)
	if err == nil {
		return accessToken, &row, false, nil
	}
	if !errors.Is(err, db.ErrVersionConflict) {
		return "", nil, false, err
	}
	// Winner may have already refreshed; reload and reuse unexpired token
	// instead of forcing another token-endpoint exchange (one-time
	// refresh_token safe).
	if reloadErr := reloadCredential(ctx, store, current); reloadErr != nil {
		return "", nil, false, reloadErr
	}
	return "", nil, true, nil
}

// reloadCredential fetches the latest persisted credential into *current.
func reloadCredential(ctx context.Context, store credentialStore, current *db.VaultCredential) error {
	reloaded, err := store.GetVaultCredential(ctx, current.WorkspaceUUID, current.VaultExternalID, current.ExternalID)
	if err != nil {
		return err
	}
	*current = reloaded
	return nil
}

func exchangeMCPOAuthRefresh(
	ctx context.Context,
	client *http.Client,
	publicAuth *mcpOAuthCredentialAuth,
	secret mcpOAuthCredentialSecret,
	now time.Time,
) (string, json.RawMessage, json.RawMessage, error) {
	if publicAuth == nil || publicAuth.Refresh == nil || secret.Refresh == nil {
		return "", nil, nil, errMCPOAuthRefreshUnavailable
	}
	publicRefresh := *publicAuth.Refresh
	secretRefresh := *secret.Refresh
	authMethodType := "none"
	clientSecret := ""
	if secretRefresh.TokenEndpointAuth != nil {
		authMethodType = strings.TrimSpace(secretRefresh.TokenEndpointAuth.Type)
		clientSecret = strings.TrimSpace(secretRefresh.TokenEndpointAuth.ClientSecret)
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", secretRefresh.RefreshToken)
	values.Set("client_id", publicRefresh.ClientID)
	if publicRefresh.Scope != nil {
		if scope := strings.TrimSpace(*publicRefresh.Scope); scope != "" {
			values.Set("scope", scope)
		}
	}
	if publicRefresh.Resource != nil {
		if resource := strings.TrimSpace(*publicRefresh.Resource); resource != "" {
			values.Set("resource", resource)
		}
	}
	method := authMethodType
	switch method {
	case "client_secret_basic":
		if clientSecret == "" {
			return "", nil, nil, tokenEndpointAuthMissingSecret("client_secret_basic")
		}
	case "client_secret_post":
		if clientSecret == "" {
			return "", nil, nil, tokenEndpointAuthMissingSecret("client_secret_post")
		}
		values.Set("client_secret", clientSecret)
	case "", "none":
		method = "none"
	default:
		return "", nil, nil, unsupportedTokenAuthMethod(method)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, publicRefresh.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if method == "client_secret_basic" {
		basic := url.QueryEscape(publicRefresh.ClientID) + ":" + url.QueryEscape(clientSecret)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(basic)))
	}
	if client == nil {
		client = defaultOAuthHTTPClient()
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
		return "", nil, nil, tokenEndpointStatus(resp.StatusCode, token.Error)
	}
	accessToken := strings.TrimSpace(token.AccessToken)
	if accessToken == "" {
		return "", nil, nil, tokenEndpointMissingAccessToken()
	}

	nextRefresh := publicRefresh
	if scope := strings.TrimSpace(token.Scope); scope != "" {
		nextRefresh.Scope = &scope
	}
	nextAuth := mcpOAuthCredentialAuth{
		Type:         credentialAuthTypeMCPOAuth,
		MCPServerURL: publicAuth.MCPServerURL,
		ExpiresAt:    resolveExpiresAtAfterRefresh(now, publicAuth.ExpiresAt, token.ExpiresIn),
		Refresh:      &nextRefresh,
	}

	nextRefreshToken := strings.TrimSpace(token.RefreshToken)
	if nextRefreshToken == "" {
		nextRefreshToken = secretRefresh.RefreshToken
	}
	nextSecret := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: accessToken,
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken:      nextRefreshToken,
			TokenEndpointAuth: secretRefresh.TokenEndpointAuth,
		},
	}

	publicJSON, err := json.Marshal(nextAuth)
	if err != nil {
		return "", nil, nil, err
	}
	secretJSON, err := json.Marshal(nextSecret)
	if err != nil {
		return "", nil, nil, err
	}
	return accessToken, json.RawMessage(publicJSON), json.RawMessage(secretJSON), nil
}

// resolveExpiresAtAfterRefresh: expires_in > 0 updates expires_at; otherwise
// keep previous only when it is still unexpired; else clear.
func resolveExpiresAtAfterRefresh(now time.Time, previous *string, expiresIn any) *string {
	if seconds := parseOAuthExpiresIn(expiresIn); seconds > 0 {
		expiresAt := now.UTC().Add(time.Duration(seconds) * time.Second).Format(time.RFC3339)
		return &expiresAt
	}
	if previous == nil {
		return nil
	}
	expired, err := accessTokenExpired(previous, now)
	if err != nil || expired {
		return nil
	}
	return previous
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
