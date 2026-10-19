package vaults

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (h *Handler) WithRefreshLease(lease OAuthRefreshLease) *Handler {
	h.validator.WithRefreshLease(lease)
	return h
}

func (i *Injector) validateMCPOAuth(ctx context.Context, credential db.VaultCredential) (validationResponse, error) {
	auth, secret, err := i.openMCPOAuthMaterial(ctx, credential)
	if err != nil {
		return validationResponse{}, err
	}
	result := validationResponse{
		CredentialID: credential.ExternalID, VaultID: credential.VaultExternalID,
		Type: "vault_credential_validation", ValidatedAt: formatTime(i.clock()),
		Status: "unknown", HasRefreshToken: secret.Refresh != nil && secret.Refresh.RefreshToken != "",
		Refresh: validationRefresh{Status: "no_refresh_token"},
	}
	capture := newValidationCapture(i.client(), auth, secret, i.platformOAuthClients)
	token := secret.AccessToken
	if result.HasRefreshToken {
		local := *i
		local.httpClient = capture.client
		token, _, err = local.refreshMCPOAuthCredential(ctx, &credential, i.clock(), true)
		result.Refresh.HTTPResponse = capture.snapshot()
		if err != nil {
			result.Refresh.Status = "connect_error"
			if result.Refresh.HTTPResponse != nil {
				result.Refresh.Status = "failed"
				result.Status = validationHTTPVerdict(result.Refresh.HTTPResponse.StatusCode)
			}
			return result, nil
		}
		result.Refresh.Status = "succeeded"
	}
	capture.values = append(capture.values, token)
	capture.response = nil
	capture.bearer = token
	client := mcp.NewClient(&mcp.Implementation{Name: "oma-vault-validation", Version: "1"}, nil)
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	session, err := client.Connect(probeCtx, &mcp.StreamableClientTransport{
		Endpoint: auth.MCPServerURL, HTTPClient: capture.client,
		MaxRetries: -1, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		result.MCPProbe = &validationProbe{Method: "initialize", HTTPResponse: capture.snapshot()}
		if result.MCPProbe.HTTPResponse != nil {
			result.Status = validationHTTPVerdict(result.MCPProbe.HTTPResponse.StatusCode)
		}
		return result, nil
	}
	_ = session.Close()
	result.Status = "valid"
	return result, nil
}

func validationHTTPVerdict(status int) string {
	if status >= 400 && status < 500 && status != http.StatusTooManyRequests && status != http.StatusRequestTimeout {
		return "invalid"
	}
	return "unknown"
}

type validationCapture struct {
	mu       sync.Mutex
	base     http.RoundTripper
	client   *http.Client
	bearer   string
	values   []string
	response *validationHTTPResponse
}

func (v *validationCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if v.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+v.bearer)
	}
	resp, err := v.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	if v.bearer != "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if req.Method == http.MethodPost {
		v.mu.Lock()
		v.response = validationSafeResponse(resp, body, v.values)
		v.mu.Unlock()
	}
	return resp, nil
}

func (v *validationCapture) snapshot() *validationHTTPResponse {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.response
}
