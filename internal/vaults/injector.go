package vaults

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

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
	db         *db.DB
	secretSvc  *secrets.Service
	logger     *slog.Logger
	httpClient *http.Client
	now        func() time.Time
	// refreshLocks serializes concurrent mcp_oauth refreshes per credential so
	// a one-time refresh_token cannot be exchanged twice in-process.
	// ponytail: one mutex per refreshed credential for process lifetime; add
	// TTL eviction if map size becomes a problem.
	refreshLocks sync.Map // credential ExternalID -> *sync.Mutex
	// store overrides db for tests; nil means use db.
	store credentialStore
}

func (i *Injector) credentialStore() credentialStore {
	if i == nil {
		return nil
	}
	if i.store != nil {
		return i.store
	}
	return i.db
}

func NewInjector(database *db.DB, secretSvc *secrets.Service, logger *slog.Logger) *Injector {
	return &Injector{
		db:        database,
		secretSvc: secretSvc,
		logger:    logging.LoggerOrDefault(logger),
	}
}

func defaultOAuthHTTPClient() *http.Client {
	// ponytail: timeout + no redirect; add SSRF-safe dialer when
	// credential-level egress policy lands.
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

func (i *Injector) refreshLock(credentialID string) *sync.Mutex {
	key := credentialID
	if key == "" {
		key = "<anonymous>"
	}
	value, _ := i.refreshLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func (i *Injector) clock() time.Time {
	if i != nil && i.now != nil {
		return i.now()
	}
	return time.Now().UTC()
}

type resolvedInjection struct {
	token      string
	credential *db.VaultCredential
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
	if t == nil || t.injector == nil {
		return t.base.RoundTrip(req)
	}
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
	for {
		result, err := t.injector.resolveFromPlan(t.ctx, plan, excluded)
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
		credID := result.credential.ExternalID
		drainAndClose(resp)
		if result.credential.AuthType == string(credentialAuthTypeMCPOAuth) {
			token, _, refreshErr := t.injector.refreshMCPOAuthCredential(t.ctx, result.credential, t.injector.clock(), true)
			if refreshErr == nil {
				retry := cloneRequestWithBody(req, body)
				retry.Header.Set("Authorization", "Bearer "+token)
				resp2, err2 := t.base.RoundTrip(retry)
				if err2 != nil {
					return resp2, err2
				}
				if resp2.StatusCode != http.StatusUnauthorized {
					return resp2, nil
				}
				drainAndClose(resp2)
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
	store := i.credentialStore()
	if store == nil {
		return injectionPlan{}, nil
	}
	vaultIDs, err := store.GetCodeSessionVaultIDs(ctx, codeSessionExternalID, organizationUUID, workspaceUUID)
	if err != nil {
		return injectionPlan{}, injectionRejected(fmt.Errorf("load vault_ids: %w", err))
	}
	if len(vaultIDs) == 0 {
		return injectionPlan{}, nil
	}
	credentials, err := store.ListActiveVaultCredentialsForVaultIDs(ctx, workspaceUUID, vaultIDs)
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
) (*resolvedInjection, error) {
	for _, cred := range plan.matches {
		if _, skip := excluded[cred.ExternalID]; skip {
			continue
		}
		result, err := i.resolveInjectableToken(ctx, cred)
		if err != nil {
			i.logger.WarnContext(ctx, "skip injectable vault credential", "credential_id", cred.ExternalID, "auth_type", cred.AuthType, "error", err)
			continue
		}
		return result, nil
	}
	if plan.hostCovered {
		return nil, injectionRejected(nil)
	}
	return nil, nil
}

func (i *Injector) resolveInjectableToken(ctx context.Context, credential *db.VaultCredential) (*resolvedInjection, error) {
	if credential == nil {
		return nil, missingCredential()
	}
	switch credential.AuthType {
	case string(credentialAuthTypeStaticBearer):
		token, err := i.openStaticBearerToken(ctx, credential)
		if err != nil {
			return nil, err
		}
		return &resolvedInjection{token: token, credential: credential}, nil
	case string(credentialAuthTypeMCPOAuth):
		return i.resolveMCPOAuthToken(ctx, credential)
	default:
		return nil, credentialTypeNotInjectable(credential.AuthType)
	}
}

func (i *Injector) resolveMCPOAuthToken(ctx context.Context, credential *db.VaultCredential) (*resolvedInjection, error) {
	current := *credential
	plaintext, err := openCredentialSecret(ctx, i.secretSvc, current)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)

	publicAuth, err := decodeMCPOAuthCredentialAuth(current.Auth)
	if err != nil {
		return nil, err
	}
	secret, err := decodeMCPOAuthCredentialSecret(plaintext)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(secret.AccessToken)
	if token == "" {
		return nil, incompleteMCPOAuthSecret()
	}
	now := i.clock()
	expired, err := accessTokenExpired(publicAuth.ExpiresAt, now)
	if err != nil {
		return nil, err
	}
	if !expired {
		return &resolvedInjection{token: token, credential: &current}, nil
	}
	token, saved, err := i.refreshMCPOAuthCredential(ctx, &current, now, false)
	if err != nil {
		return nil, err
	}
	return &resolvedInjection{token: token, credential: saved}, nil
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

func snapshotRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return io.ReadAll(io.LimitReader(body, 32<<20))
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, 32<<20))
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
