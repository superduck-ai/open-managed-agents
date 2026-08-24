package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const maxOAuthRefreshCASAttempts = 3

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
	release, err := i.refreshLease.Hold(ctx, credential.ExternalID)
	if err != nil {
		return "", nil, err
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			i.logger.WarnContext(ctx, "release mcp oauth refresh lease", "credential_id", credential.ExternalID, "error", releaseErr)
		}
	}()

	current := *credential
	// Best-effort re-read under the per-credential lease so a concurrent winner's
	// token is visible before we exchange (one-time refresh_token safe). A failed
	// reload keeps the caller snapshot and continues.
	if err := reloadCredential(ctx, i.store, &current); err != nil {
		i.logger.DebugContext(ctx, "mcp_oauth refresh preload miss", "credential_id", credential.ExternalID, "error", err)
	}
	for attempt := 0; attempt < maxOAuthRefreshCASAttempts; attempt++ {
		token, saved, retry, err := i.refreshMCPOAuthAttempt(ctx, i.store, &current, now, force)
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
	publicAuth, secret, err := i.openMCPOAuthMaterial(ctx, *current)
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
	accessToken, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(ctx, i.client(), publicAuth, secret, now, i.platformOAuthClients)
	if err != nil {
		// Exchange failed (e.g. invalid_grant after a concurrent winner
		// consumed the refresh_token). Reload and retry only when the
		// persisted envelope advanced; otherwise keep the exchange error.
		before := current.SecretVersion
		if reloadErr := reloadCredential(ctx, store, current); reloadErr != nil {
			return "", nil, false, err
		}
		if current.SecretVersion == before {
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
	clients []config.PlatformOAuthClientConfig,
) (string, json.RawMessage, json.RawMessage, error) {
	if publicAuth == nil || publicAuth.Refresh == nil || secret.Refresh == nil {
		return "", nil, nil, errMCPOAuthRefreshUnavailable
	}
	publicRefresh := *publicAuth.Refresh
	secretRefresh := *secret.Refresh
	authMethod := "none"
	if secretRefresh.TokenEndpointAuth != nil {
		authMethod = strings.TrimSpace(secretRefresh.TokenEndpointAuth.Type)
	}
	source := mcpOAuthRefreshClientCredentialSource(publicAuth, secret)
	clientSecret, err := resolveMCPOAuthRefreshClientSecret(publicAuth, secret, clients)
	if err != nil {
		return "", nil, nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", secretRefresh.RefreshToken)
	form.Set("client_id", publicRefresh.ClientID)
	if publicRefresh.Scope != nil {
		if scope := strings.TrimSpace(*publicRefresh.Scope); scope != "" {
			form.Set("scope", scope)
		}
	}
	if publicRefresh.Resource != nil {
		if resource := strings.TrimSpace(*publicRefresh.Resource); resource != "" {
			form.Set("resource", resource)
		}
	}

	token, err := ExchangeOAuthTokenEndpoint(ctx, client, OAuthTokenEndpointExchange{
		TokenEndpoint:           publicRefresh.TokenEndpoint,
		ClientID:                publicRefresh.ClientID,
		ClientSecret:            clientSecret,
		TokenEndpointAuthMethod: authMethod,
		Form:                    form,
	})
	if err != nil {
		return "", nil, nil, err
	}

	nextRefresh := publicRefresh
	if scope := strings.TrimSpace(token.Scope); scope != "" {
		nextRefresh.Scope = &scope
	}
	nextAuth := mcpOAuthCredentialAuth{
		Type:                   credentialAuthTypeMCPOAuth,
		MCPServerURL:           publicAuth.MCPServerURL,
		ClientCredentialSource: source,
		ExpiresAt:              resolveExpiresAtAfterRefresh(now, publicAuth.ExpiresAt, token.ExpiresIn),
		Refresh:                &nextRefresh,
	}

	nextRefreshToken := strings.TrimSpace(token.RefreshToken)
	if nextRefreshToken == "" {
		nextRefreshToken = secretRefresh.RefreshToken
	}
	nextTokenAuth := &tokenEndpointAuthSecret{Type: authMethod}
	if persistSecret := ClientSecretForMCPOAuthPersist(source, clientSecret); persistSecret != "" {
		nextTokenAuth.ClientSecret = persistSecret
	}
	nextSecret := mcpOAuthCredentialSecret{
		Type:        credentialAuthTypeMCPOAuth,
		AccessToken: token.AccessToken,
		Refresh: &mcpOAuthRefreshSecret{
			RefreshToken:      nextRefreshToken,
			TokenEndpointAuth: nextTokenAuth,
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
	return token.AccessToken, json.RawMessage(publicJSON), json.RawMessage(secretJSON), nil
}

// resolveExpiresAtAfterRefresh: expires_in > 0 updates expires_at; otherwise
// keep previous only when it is still unexpired; else clear.
func resolveExpiresAtAfterRefresh(now time.Time, previous *string, expiresIn OAuthExpiresIn) *string {
	if expiresIn > 0 {
		expiresAt := now.UTC().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
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
