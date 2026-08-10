package vaults

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

var ErrInjectionRejected = errors.New("vault credential injection rejected")

// Injector loads session vault credentials per request and rewrites MCP
// Authorization for injectable targets. Plaintext tokens are never cached.
type Injector struct {
	db         *db.DB
	secretSvc  *secrets.Service
	httpClient *http.Client
	now        func() time.Time
}

func NewInjector(database *db.DB, secretSvc *secrets.Service) *Injector {
	return &Injector{db: database, secretSvc: secretSvc}
}

func (i *Injector) client() *http.Client {
	if i != nil && i.httpClient != nil {
		return i.httpClient
	}
	return http.DefaultClient
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

// RewriteAuthorization: passthrough leaves headers alone; inject sets Bearer;
// reject returns ErrInjectionRejected. Open/refresh failures skip to the next
// matching injectable credential.
func (i *Injector) RewriteAuthorization(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
	requestURL *url.URL,
	header http.Header,
) error {
	result, err := i.resolveAuthorization(ctx, codeSessionExternalID, organizationUUID, workspaceUUID, requestURL, nil)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	header.Set("Authorization", "Bearer "+result.token)
	return nil
}

// WrapTransport returns a RoundTripper that injects vault credentials and, for
// mcp_oauth, performs one refresh+retry on upstream 401 before skipping to the
// next matching credential.
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
	excluded := map[string]struct{}{}
	for {
		result, err := t.injector.resolveAuthorization(
			t.ctx,
			t.codeSessionExternalID,
			t.organizationUUID,
			t.workspaceUUID,
			t.requestURL,
			excluded,
		)
		if err != nil {
			return nil, err
		}
		out := req.Clone(req.Context())
		restoreRequestBody(out, body)
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
		if result.credential.AuthType == "mcp_oauth" {
			refreshed, refreshErr := t.injector.refreshAfterUnauthorized(t.ctx, result.credential)
			if refreshErr == nil && refreshed != nil {
				drainAndClose(resp)
				retry := req.Clone(req.Context())
				restoreRequestBody(retry, body)
				retry.Header.Set("Authorization", "Bearer "+refreshed.token)
				resp2, err2 := t.base.RoundTrip(retry)
				if err2 != nil {
					return resp2, err2
				}
				if resp2.StatusCode != http.StatusUnauthorized {
					return resp2, nil
				}
				drainAndClose(resp2)
			} else {
				drainAndClose(resp)
			}
		} else {
			drainAndClose(resp)
		}
		excluded[credID] = struct{}{}
	}
}

func (i *Injector) resolveAuthorization(
	ctx context.Context,
	codeSessionExternalID string,
	organizationUUID string,
	workspaceUUID string,
	requestURL *url.URL,
	excluded map[string]struct{},
) (*resolvedInjection, error) {
	if i == nil || i.db == nil {
		return nil, nil
	}
	vaultIDs, err := i.db.GetCodeSessionVaultIDs(ctx, codeSessionExternalID, organizationUUID, workspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("%w: load vault_ids: %w", ErrInjectionRejected, err)
	}
	if len(vaultIDs) == 0 {
		return nil, nil
	}
	credentials, err := i.db.ListActiveVaultCredentialsForVaultIDs(ctx, workspaceUUID, vaultIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: load credentials: %w", ErrInjectionRejected, err)
	}
	matches, hostCovered, err := listInjectableMatches(requestURL, credentials)
	if err != nil {
		return nil, ErrInjectionRejected
	}
	for _, cred := range matches {
		if excluded != nil {
			if _, skip := excluded[cred.ExternalID]; skip {
				continue
			}
		}
		token, resolvedCred, err := i.resolveInjectableToken(ctx, cred)
		if err != nil {
			continue
		}
		return &resolvedInjection{token: token, credential: resolvedCred}, nil
	}
	if hostCovered {
		return nil, ErrInjectionRejected
	}
	return nil, nil
}

func (i *Injector) resolveInjectableToken(ctx context.Context, credential *db.VaultCredential) (string, *db.VaultCredential, error) {
	if credential == nil {
		return "", nil, errors.New("missing credential")
	}
	switch credential.AuthType {
	case "static_bearer":
		token, err := i.openStaticBearerToken(ctx, credential)
		if err != nil {
			return "", nil, err
		}
		return token, credential, nil
	case "mcp_oauth":
		return i.resolveMCPOAuthToken(ctx, credential)
	default:
		return "", nil, fmt.Errorf("credential type %q is not injectable", credential.AuthType)
	}
}

func (i *Injector) resolveMCPOAuthToken(ctx context.Context, credential *db.VaultCredential) (string, *db.VaultCredential, error) {
	current := *credential
	plaintext, err := openCredentialSecret(ctx, i.secretSvc, current)
	if err != nil {
		return "", nil, err
	}
	defer clear(plaintext)

	publicAuth, err := parseMCPOAuthPublicAuth(current.Auth)
	if err != nil {
		return "", nil, err
	}
	secret, err := parseMCPOAuthSecret(plaintext)
	if err != nil {
		return "", nil, err
	}
	token := strings.TrimSpace(secret.AccessToken)
	if token == "" || secret.Type != "mcp_oauth" {
		return "", nil, errors.New("mcp_oauth secret payload is incomplete")
	}
	expired, err := accessTokenExpired(publicAuth.ExpiresAt, i.clock())
	if err != nil {
		return "", nil, err
	}
	if !expired {
		return token, &current, nil
	}
	return i.refreshMCPOAuthCredential(ctx, &current, i.clock(), false)
}

func (i *Injector) refreshAfterUnauthorized(ctx context.Context, credential *db.VaultCredential) (*resolvedInjection, error) {
	if credential == nil || credential.AuthType != "mcp_oauth" {
		return nil, errMCPOAuthRefreshUnavailable
	}
	token, saved, err := i.refreshMCPOAuthCredential(ctx, credential, i.clock(), true)
	if err != nil {
		return nil, err
	}
	return &resolvedInjection{token: token, credential: saved}, nil
}

func (i *Injector) openStaticBearerToken(ctx context.Context, credential *db.VaultCredential) (string, error) {
	if credential == nil {
		return "", errors.New("missing credential")
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
