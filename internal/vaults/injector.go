package vaults

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

// oauthRefreshTimeout bounds a single token-endpoint exchange so a hung IdP
// cannot stall an MCP RoundTrip.
const oauthRefreshTimeout = 15 * time.Second

// credentialStore is the test seam for the Injector's persistence surface
// (refresh CAS + session credential loading). *db.DB satisfies it implicitly;
// production only ever uses *db.DB. Tests substitute a fake to inject
// deterministic credentials without a live database.
type credentialStore interface {
	UpdateVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string, next db.VaultCredential) (db.VaultCredential, error)
	GetVaultCredential(ctx context.Context, workspaceUUID, vaultExternalID, credentialExternalID string) (db.VaultCredential, error)
	GetCodeSessionVaultIDs(ctx context.Context, codeSessionExternalID, organizationUUID, workspaceUUID string) ([]string, error)
	ListActiveVaultCredentialsForVaultIDs(ctx context.Context, workspaceUUID string, vaultExternalIDs []string) ([]db.VaultCredential, error)
}

// Injector loads session vault credentials per request and rewrites MCP
// Authorization for injectable targets. Plaintext tokens are never cached.
type Injector struct {
	store                credentialStore
	secretSvc            *secrets.Service
	logger               *slog.Logger
	httpClient           *http.Client
	now                  func() time.Time
	refreshLease         OAuthRefreshLease
	platformOAuthClients []config.PlatformOAuthClientConfig
}

func NewInjector(database *db.DB, secretSvc *secrets.Service, logger *slog.Logger) *Injector {
	var store credentialStore
	if database != nil {
		store = database
	}
	return &Injector{
		store:        store,
		secretSvc:    secretSvc,
		logger:       logging.LoggerOrDefault(logger),
		refreshLease: newMemoryOAuthRefreshLease(),
	}
}

// WithRefreshLease replaces the in-process lease used by tests with a
// cross-instance adapter built at assembly (typically Redis).
func (i *Injector) WithRefreshLease(lease OAuthRefreshLease) *Injector {
	if i == nil || lease == nil {
		return i
	}
	i.refreshLease = lease
	return i
}

// WithPlatformOAuthClients supplies vault.platform_oauth_clients so mcp_oauth
// refresh can re-resolve deploy-config secrets that were never sealed.
func (i *Injector) WithPlatformOAuthClients(clients []config.PlatformOAuthClientConfig) *Injector {
	if i == nil {
		return i
	}
	i.platformOAuthClients = clients
	return i
}

func defaultOAuthHTTPClient() *http.Client {
	return &http.Client{
		Timeout: oauthRefreshTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (i *Injector) client() *http.Client {
	if i != nil && i.httpClient != nil {
		return i.httpClient
	}
	return defaultOAuthHTTPClient()
}

func (i *Injector) clock() time.Time {
	if i != nil && i.now != nil {
		return i.now()
	}
	return time.Now().UTC()
}

type resolvedInjection struct {
	token      string
	planCredID string
	authType   string
}

// injectionPlan is the vault_ids-ordered match set for one outbound MCP URL.
// Loaded once per RoundTrip; walk/401 retries reuse it without re-querying.
type injectionPlan struct {
	matches     []*db.VaultCredential
	hostCovered bool
}

func (i *Injector) WrapTransport(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
	requestURL *url.URL,
	base http.RoundTripper,
) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &injectingRoundTripper{
		injector:              i,
		base:                  base,
		ctx:                   ctx,
		codeSessionExternalID: codeSessionExternalID,
		organizationUUID:      organizationUUID,
		workspaceUUID:         workspaceUUID,
		requestURL:            requestURL,
	}
}

type injectingRoundTripper struct {
	injector              *Injector
	base                  http.RoundTripper
	ctx                   context.Context
	codeSessionExternalID string
	organizationUUID      string
	workspaceUUID         string
	requestURL            *url.URL
}

func (t *injectingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil {
		return http.DefaultTransport.RoundTrip(req)
	}
	if t.injector == nil {
		return t.base.RoundTrip(req)
	}
	// RoundTripper contract: always close the caller's body. Clones go to base;
	// snapshot may replace req.Body with a restored buffer — close whichever is
	// left on req when this returns.
	defer closeRequestBody(req)
	body, err := snapshotRequestBody(req)
	if err != nil {
		return nil, err
	}
	plan, err := t.injector.loadInjectionPlan(
		t.ctx,
		t.codeSessionExternalID,
		t.organizationUUID,
		t.workspaceUUID,
		t.requestURL,
	)
	if err != nil {
		return nil, err
	}
	excluded := map[string]struct{}{}
	// forceRefresh marks mcp_oauth credentials that already returned 401 once;
	// the next resolve forces a token-endpoint refresh before retrying inject.
	forceRefresh := map[string]struct{}{}
	for {
		result, err := t.injector.resolveFromPlan(t.ctx, plan, excluded, forceRefresh)
		if err != nil {
			return nil, err
		}
		out := cloneRequestWithBody(req, body)
		if result == nil {
			return t.base.RoundTrip(out)
		}
		out.Header.Set("Authorization", "Bearer "+result.token)
		resp, err := t.base.RoundTrip(out)
		if err != nil {
			return resp, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		credID := result.planCredID
		drainAndClose(resp)
		if credentialAuthType(result.authType) == credentialAuthTypeMCPOAuth {
			if _, alreadyForced := forceRefresh[credID]; !alreadyForced {
				forceRefresh[credID] = struct{}{}
				continue
			}
		}
		excluded[credID] = struct{}{}
	}
}

func (i *Injector) loadInjectionPlan(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
	requestURL *url.URL,
) (injectionPlan, error) {
	vaultIDs, err := i.store.GetCodeSessionVaultIDs(ctx, codeSessionExternalID, organizationUUID, workspaceUUID)
	if err != nil {
		return injectionPlan{}, injectionRejected(fmt.Errorf("load vault_ids: %w", err))
	}
	if len(vaultIDs) == 0 {
		return injectionPlan{}, nil
	}
	credentials, err := i.store.ListActiveVaultCredentialsForVaultIDs(ctx, workspaceUUID, vaultIDs)
	if err != nil {
		return injectionPlan{}, injectionRejected(fmt.Errorf("load credentials: %w", err))
	}
	matches, hostCovered, err := listInjectableMatches(requestURL, credentials)
	if err != nil {
		return injectionPlan{}, injectionRejected(fmt.Errorf("match credentials: %w", err))
	}
	return injectionPlan{matches: matches, hostCovered: hostCovered}, nil
}

func (i *Injector) resolveFromPlan(
	ctx context.Context,
	plan injectionPlan,
	excluded map[string]struct{},
	forceRefresh map[string]struct{},
) (*resolvedInjection, error) {
	for _, cred := range plan.matches {
		if _, skip := excluded[cred.ExternalID]; skip {
			continue
		}
		_, force := forceRefresh[cred.ExternalID]
		result, err := i.resolveInjectableToken(ctx, cred, force)
		if err != nil {
			i.logger.WarnContext(ctx, "skip injectable vault credential", "credential_id", cred.ExternalID, "auth_type", cred.AuthType, "error", err)
			continue
		}
		result.planCredID = cred.ExternalID
		result.authType = cred.AuthType
		return result, nil
	}
	if plan.hostCovered {
		return nil, injectionRejected(nil)
	}
	return nil, nil
}

func (i *Injector) resolveInjectableToken(ctx context.Context, credential *db.VaultCredential, forceRefresh bool) (*resolvedInjection, error) {
	if credential == nil {
		return nil, missingCredential()
	}
	switch credentialAuthType(credential.AuthType) {
	case credentialAuthTypeStaticBearer:
		token, err := i.openStaticBearerToken(ctx, credential)
		if err != nil {
			return nil, err
		}
		return &resolvedInjection{token: token}, nil
	case credentialAuthTypeMCPOAuth:
		return i.resolveMCPOAuthToken(ctx, credential, forceRefresh)
	default:
		return nil, credentialTypeNotInjectable(credential.AuthType)
	}
}

func (i *Injector) resolveMCPOAuthToken(ctx context.Context, credential *db.VaultCredential, forceRefresh bool) (*resolvedInjection, error) {
	current := *credential
	now := i.clock()
	if !forceRefresh {
		// Expiry lives on public auth; only open the envelope when the stored
		// access token can still be reused. Expired / force paths open once
		// inside refresh under the per-credential lock (after reload).
		publicAuth, err := decodeMCPOAuthCredentialAuth(current.Auth)
		if err != nil {
			return nil, err
		}
		expired, err := accessTokenExpired(publicAuth.ExpiresAt, now)
		if err != nil {
			return nil, err
		}
		if !expired {
			plaintext, err := openCredentialSecret(ctx, i.secretSvc, current)
			if err != nil {
				return nil, err
			}
			defer clear(plaintext)
			secret, err := decodeMCPOAuthCredentialSecret(plaintext)
			if err != nil {
				return nil, err
			}
			if secret.AccessToken == "" {
				return nil, incompleteMCPOAuthSecret()
			}
			return &resolvedInjection{token: secret.AccessToken}, nil
		}
	}
	token, _, err := i.refreshMCPOAuthCredential(ctx, &current, now, forceRefresh)
	if err != nil {
		return nil, err
	}
	return &resolvedInjection{token: token}, nil
}

// openMCPOAuthMaterial opens the envelope and decodes mcp_oauth public auth +
// secret. Plaintext is cleared before return; token strings are owned copies.
func (i *Injector) openMCPOAuthMaterial(ctx context.Context, credential db.VaultCredential) (*mcpOAuthCredentialAuth, mcpOAuthCredentialSecret, error) {
	plaintext, err := openCredentialSecret(ctx, i.secretSvc, credential)
	if err != nil {
		return nil, mcpOAuthCredentialSecret{}, err
	}
	defer clear(plaintext)

	publicAuth, err := decodeMCPOAuthCredentialAuth(credential.Auth)
	if err != nil {
		return nil, mcpOAuthCredentialSecret{}, err
	}
	secret, err := decodeMCPOAuthCredentialSecret(plaintext)
	if err != nil {
		return nil, mcpOAuthCredentialSecret{}, err
	}
	return publicAuth, secret, nil
}

func (i *Injector) openStaticBearerToken(ctx context.Context, credential *db.VaultCredential) (string, error) {
	if credential == nil {
		return "", missingCredential()
	}
	plaintext, err := openCredentialSecret(ctx, i.secretSvc, *credential)
	if err != nil {
		return "", err
	}
	defer clear(plaintext)

	secret, err := decodeStaticBearerCredentialSecret(plaintext)
	if err != nil {
		return "", err
	}
	return secret.Token, nil
}

func cloneRequestWithBody(req *http.Request, body []byte) *http.Request {
	out := req.Clone(req.Context())
	restoreRequestBody(out, body)
	return out
}

// maxSnapshotRequestBodyBytes caps buffered request bodies for MCP 401 retry
// and Environment Variable body substitution. Larger bodies fail closed
// instead of silently truncating replay or forwarding an unsubstituted placeholder.
const maxSnapshotRequestBodyBytes = 32 << 20

var errSnapshotRequestBodyTooLarge = fmt.Errorf("request body exceeds %d-byte snapshot buffer", maxSnapshotRequestBodyBytes)

func readWithinLimit(r io.Reader, max int64) ([]byte, error) {
	if max < 0 {
		max = 0
	}
	data, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errSnapshotRequestBodyTooLarge
	}
	return data, nil
}

func closeRequestBody(req *http.Request) {
	if req == nil || req.Body == nil || req.Body == http.NoBody {
		return
	}
	_ = req.Body.Close()
	req.Body = http.NoBody
}

func snapshotRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.ContentLength > maxSnapshotRequestBodyBytes {
		return nil, errSnapshotRequestBodyTooLarge
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return readWithinLimit(body, maxSnapshotRequestBodyBytes)
	}
	data, err := readWithinLimit(req.Body, maxSnapshotRequestBodyBytes)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	restoreRequestBody(req, data)
	return data, nil
}

func restoreRequestBody(req *http.Request, body []byte) {
	if body == nil {
		req.Body = http.NoBody
		req.ContentLength = 0
		req.GetBody = nil
		return
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
}
