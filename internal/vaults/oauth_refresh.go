package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const maxOAuthRefreshCASAttempts = 3

func accessTokenExpired(expiresAt *string, now time.Time) (bool, error) {
	if expiresAt == nil {
		return false, nil
	}
	value := *expiresAt
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
	return auth.Refresh.TokenEndpoint != "" &&
		auth.Refresh.ClientID != "" &&
		secret.Refresh.RefreshToken != ""
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
	return i.refreshMCPOAuthAttempt(ctx, i.store, &current, now, force)
}

// refreshMCPOAuthAttempt opens, exchanges at most once, then persists or reuses.
func (i *Injector) refreshMCPOAuthAttempt(
	ctx context.Context,
	store credentialStore,
	current *db.VaultCredential,
	now time.Time,
	force bool,
) (token string, saved *db.VaultCredential, err error) {
	publicAuth, secret, err := i.openMCPOAuthMaterial(ctx, *current)
	if err != nil {
		return "", nil, err
	}
	usable, err := mcpOAuthAccessUsable(publicAuth, secret, now)
	if err != nil {
		return "", nil, err
	}
	if !force && usable {
		return secret.AccessToken, current, nil
	}
	if !hasMCPOAuthRefreshMaterial(publicAuth, secret) {
		return "", nil, errMCPOAuthRefreshUnavailable
	}
	accessToken, nextAuth, nextSecret, err := exchangeMCPOAuthRefresh(ctx, i.client(), publicAuth, secret, now, i.platformOAuthClients)
	if err != nil {
		return i.finishFailedMCPOAuthExchange(ctx, store, current, secret, now, err)
	}
	return i.persistExchangedMCPOAuth(ctx, store, current, secret, accessToken, nextAuth, nextSecret, now)
}

// finishFailedMCPOAuthExchange reloads after a token-endpoint error. A replaced
// envelope with a usable access token is reused; an unchanged envelope (including
// rename) keeps the exchange error. This Hold never exchanges again.
func (i *Injector) finishFailedMCPOAuthExchange(
	ctx context.Context,
	store credentialStore,
	current *db.VaultCredential,
	exchangedFrom mcpOAuthCredentialSecret,
	now time.Time,
	exchangeErr error,
) (string, *db.VaultCredential, error) {
	if reloadErr := reloadCredential(ctx, store, current); reloadErr != nil {
		return "", nil, exchangeErr
	}
	token, ok, err := i.usableReloadedMCPOAuth(ctx, current, exchangedFrom, now)
	if err != nil || !ok {
		return "", nil, exchangeErr
	}
	return token, current, nil
}

// persistExchangedMCPOAuth writes an already-exchanged token. On version
// conflict it rebases onto the latest row unless another writer left a usable
// envelope, which is reused. This Hold never exchanges again.
func (i *Injector) persistExchangedMCPOAuth(
	ctx context.Context,
	store credentialStore,
	current *db.VaultCredential,
	exchangedFrom mcpOAuthCredentialSecret,
	accessToken string,
	nextAuth, nextSecret json.RawMessage,
	now time.Time,
) (string, *db.VaultCredential, error) {
	for range maxOAuthRefreshCASAttempts {
		updated := *current
		updated.Auth = nextAuth
		updated.SecretPayload = nextSecret
		updated.UpdatedAt = now.UTC()
		if err := SealCredentialSecret(ctx, i.secretSvc, &updated); err != nil {
			return "", nil, err
		}
		row, err := store.UpdateVaultCredential(ctx, updated.WorkspaceUUID, updated.VaultExternalID, updated.ExternalID, updated)
		if err == nil {
			return accessToken, &row, nil
		}
		if !errors.Is(err, db.ErrVersionConflict) {
			return "", nil, err
		}
		if reloadErr := reloadCredential(ctx, store, current); reloadErr != nil {
			return "", nil, reloadErr
		}
		token, ok, err := i.usableReloadedMCPOAuth(ctx, current, exchangedFrom, now)
		if err != nil {
			return "", nil, err
		}
		if ok {
			return token, current, nil
		}
	}
	return "", nil, errMCPOAuthRefreshUnavailable
}

func (i *Injector) usableReloadedMCPOAuth(
	ctx context.Context,
	current *db.VaultCredential,
	exchangedFrom mcpOAuthCredentialSecret,
	now time.Time,
) (token string, usable bool, err error) {
	publicAuth, secret, err := i.openMCPOAuthMaterial(ctx, *current)
	if err != nil {
		return "", false, err
	}
	if mcpOAuthSecretSourceEqual(exchangedFrom, secret) {
		return "", false, nil
	}
	ok, err := mcpOAuthAccessUsable(publicAuth, secret, now)
	if err != nil || !ok {
		return "", false, err
	}
	return secret.AccessToken, true, nil
}

func mcpOAuthAccessUsable(auth *mcpOAuthCredentialAuth, secret mcpOAuthCredentialSecret, now time.Time) (bool, error) {
	if secret.AccessToken == "" {
		return false, nil
	}
	var expiresAt *string
	if auth != nil {
		expiresAt = auth.ExpiresAt
	}
	expired, err := accessTokenExpired(expiresAt, now)
	if err != nil {
		return false, err
	}
	return !expired, nil
}

func mcpOAuthSecretSourceEqual(left, right mcpOAuthCredentialSecret) bool {
	return left.AccessToken == right.AccessToken && mcpOAuthRefreshToken(left) == mcpOAuthRefreshToken(right)
}

func mcpOAuthRefreshToken(secret mcpOAuthCredentialSecret) string {
	if secret.Refresh == nil {
		return ""
	}
	return secret.Refresh.RefreshToken
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
	if secretRefresh.TokenEndpointAuth != nil && secretRefresh.TokenEndpointAuth.Type != "" {
		authMethod = secretRefresh.TokenEndpointAuth.Type
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
	if publicRefresh.Scope != nil && *publicRefresh.Scope != "" {
		form.Set("scope", *publicRefresh.Scope)
	}
	if publicRefresh.Resource != nil && *publicRefresh.Resource != "" {
		form.Set("resource", *publicRefresh.Resource)
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
	if token.Scope != "" {
		scope := token.Scope
		nextRefresh.Scope = &scope
	}
	nextAuth := mcpOAuthCredentialAuth{
		Type:                   credentialAuthTypeMCPOAuth,
		MCPServerURL:           publicAuth.MCPServerURL,
		ClientCredentialSource: source,
		ExpiresAt:              resolveExpiresAtAfterRefresh(now, publicAuth.ExpiresAt, token.ExpiresIn),
		Refresh:                &nextRefresh,
	}

	nextRefreshToken := token.RefreshToken
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
