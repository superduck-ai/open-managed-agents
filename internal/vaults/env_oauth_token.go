package vaults

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var errEnvOAuthRefreshUnavailable = errors.New("env_oauth refresh unavailable")

type envOAuthTokenResolver struct {
	store        credentialStore
	secretSvc    *secrets.Service
	logger       *slog.Logger
	client       *http.Client
	now          func() time.Time
	refreshLease OAuthRefreshLease
	providers    EnvOAuthProviderResolver
}

func newEnvOAuthTokenResolver(
	store credentialStore,
	secretSvc *secrets.Service,
	logger *slog.Logger,
	refreshLease OAuthRefreshLease,
) *envOAuthTokenResolver {
	if refreshLease == nil {
		refreshLease = newMemoryOAuthRefreshLease()
	}
	return &envOAuthTokenResolver{
		store:        store,
		secretSvc:    secretSvc,
		logger:       logging.LoggerOrDefault(logger),
		refreshLease: refreshLease,
	}
}

func (r *envOAuthTokenResolver) withProviders(providers EnvOAuthProviderResolver) *envOAuthTokenResolver {
	if r == nil {
		return nil
	}
	r.providers = providers
	return r
}

func (r *envOAuthTokenResolver) withHTTPClient(client *http.Client) *envOAuthTokenResolver {
	if r == nil {
		return nil
	}
	r.client = client
	return r
}

func (r *envOAuthTokenResolver) clock() time.Time {
	if r != nil && r.now != nil {
		return r.now()
	}
	return time.Now().UTC()
}

func (r *envOAuthTokenResolver) httpClient() *http.Client {
	if r != nil && r.client != nil {
		return r.client
	}
	return defaultOAuthHTTPClient()
}

func (r *envOAuthTokenResolver) resolve(ctx context.Context, credential *db.VaultCredential) (string, error) {
	if r == nil || credential == nil {
		return "", substitutionRejected(errEnvOAuthRefreshUnavailable)
	}
	current := *credential
	now := r.clock()
	token, usable, err := r.usableToken(ctx, current, now)
	if err != nil {
		return "", err
	}
	if usable {
		return token, nil
	}

	ctx, release, err := r.refreshLease.Hold(ctx, current.ExternalID)
	if err != nil {
		return "", err
	}
	defer func() {
		if releaseErr := release(); releaseErr != nil {
			r.logger.WarnContext(ctx, "release env oauth refresh lease", "credential_id", current.ExternalID, "error", releaseErr)
		}
	}()

	if err := reloadCredential(ctx, r.store, &current); err != nil {
		return "", err
	}
	token, usable, err = r.usableToken(ctx, current, now)
	if err != nil {
		return "", err
	}
	if usable {
		return token, nil
	}
	return r.refreshAndPersist(ctx, &current, now)
}

func (r *envOAuthTokenResolver) usableToken(ctx context.Context, credential db.VaultCredential, now time.Time) (string, bool, error) {
	publicAuth, secret, err := r.openMaterial(ctx, credential)
	if err != nil {
		return "", false, err
	}
	if secret.AccessToken == "" {
		return "", false, nil
	}
	expired, err := accessTokenExpired(publicAuth.ExpiresAt, now)
	if err != nil {
		return "", false, err
	}
	if expired {
		return "", false, nil
	}
	return secret.AccessToken, true, nil
}

func (r *envOAuthTokenResolver) openMaterial(ctx context.Context, credential db.VaultCredential) (*envOAuthCredentialAuth, envOAuthCredentialSecret, error) {
	plaintext, err := openCredentialSecret(ctx, r.secretSvc, credential)
	if err != nil {
		return nil, envOAuthCredentialSecret{}, err
	}
	defer clear(plaintext)

	publicAuth, err := decodeEnvOAuthCredentialAuth(credential.Auth)
	if err != nil {
		return nil, envOAuthCredentialSecret{}, err
	}
	secret, err := decodeEnvOAuthCredentialSecret(plaintext)
	if err != nil {
		return nil, envOAuthCredentialSecret{}, err
	}
	return publicAuth, secret, nil
}

func (r *envOAuthTokenResolver) refreshAndPersist(ctx context.Context, current *db.VaultCredential, now time.Time) (string, error) {
	publicAuth, secret, err := r.openMaterial(ctx, *current)
	if err != nil {
		return "", err
	}
	if secret.RefreshToken == "" {
		return "", errors.Join(ErrEnvOAuthReauthorizationRequired, errEnvOAuthRefreshUnavailable)
	}
	if r.providers == nil {
		return "", errors.Join(ErrEnvOAuthProviderUnavailable, errEnvOAuthRefreshUnavailable)
	}
	provider, err := r.providers.ResolveEnvOAuthProvider(ctx, publicAuth.Provider)
	if err != nil || provider.TokenEndpoint == "" || provider.ClientID == "" {
		return "", errors.Join(ErrEnvOAuthProviderUnavailable, err, errEnvOAuthRefreshUnavailable)
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", secret.RefreshToken)
	form.Set("client_id", provider.ClientID)
	switch {
	case publicAuth.Scope != nil && *publicAuth.Scope != "":
		form.Set("scope", *publicAuth.Scope)
	case provider.Scope != "":
		form.Set("scope", provider.Scope)
	}
	token, err := ExchangeOAuthTokenEndpoint(ctx, r.httpClient(), OAuthTokenEndpointExchange{
		TokenEndpoint:           provider.TokenEndpoint,
		ClientID:                provider.ClientID,
		ClientSecret:            provider.ClientSecret,
		TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   provider.TokenEndpointEncoding,
		Form:                    form,
	})
	if err != nil {
		if permanentOAuthRefreshFailure(err) {
			return "", errors.Join(ErrEnvOAuthReauthorizationRequired, err)
		}
		return "", err
	}

	refreshToken := token.RefreshToken
	if refreshToken == "" {
		refreshToken = secret.RefreshToken
	}
	nextAuth := *publicAuth
	nextAuth.ExpiresAt = resolveExpiresAtAfterRefresh(now, publicAuth.ExpiresAt, token.ExpiresIn)
	if token.Scope != "" {
		scope := token.Scope
		nextAuth.Scope = &scope
	} else if nextAuth.Scope == nil && provider.Scope != "" {
		scope := provider.Scope
		nextAuth.Scope = &scope
	}
	nextAuth.HasRefreshToken = refreshToken != ""
	nextSecret := envOAuthCredentialSecret{
		Type:         credentialAuthTypeEnvOAuth,
		AccessToken:  token.AccessToken,
		RefreshToken: refreshToken,
	}
	nextAuthJSON, err := json.Marshal(nextAuth)
	if err != nil {
		return "", err
	}
	nextSecretJSON, err := json.Marshal(nextSecret)
	if err != nil {
		return "", err
	}
	return r.persist(ctx, current, secret, nextAuthJSON, nextSecretJSON, token.AccessToken, now)
}

func (r *envOAuthTokenResolver) persist(
	ctx context.Context,
	current *db.VaultCredential,
	exchangedSecret envOAuthCredentialSecret,
	nextAuth, nextSecret json.RawMessage,
	accessToken string,
	now time.Time,
) (string, error) {
	for range maxOAuthRefreshCASAttempts {
		if err := ctx.Err(); err != nil {
			return "", context.Cause(ctx)
		}
		updated := *current
		updated.Auth = nextAuth
		updated.SecretPayload = append(json.RawMessage(nil), nextSecret...)
		updated.UpdatedAt = now.UTC()
		if err := SealCredentialSecret(ctx, r.secretSvc, &updated); err != nil {
			clearCredentialSecretPayload(&updated)
			return "", err
		}
		_, err := r.store.UpdateVaultCredential(ctx, updated.WorkspaceUUID, updated.VaultExternalID, updated.ExternalID, updated)
		if err == nil {
			return accessToken, nil
		}
		if !errors.Is(err, db.ErrVersionConflict) {
			return "", err
		}
		if reloadErr := reloadCredential(ctx, r.store, current); reloadErr != nil {
			return "", reloadErr
		}
		publicAuth, secret, openErr := r.openMaterial(ctx, *current)
		if openErr != nil {
			return "", openErr
		}
		if secret.AccessToken != "" && secret.AccessToken != exchangedSecret.AccessToken {
			expired, expiryErr := accessTokenExpired(publicAuth.ExpiresAt, now)
			if expiryErr != nil {
				return "", expiryErr
			}
			if !expired {
				return secret.AccessToken, nil
			}
		}
	}
	return "", errEnvOAuthRefreshUnavailable
}
