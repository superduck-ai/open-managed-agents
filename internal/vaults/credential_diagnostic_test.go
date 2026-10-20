package vaults

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCredentialDiagnosticPreservesSuccessfulSSEStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	capture := newValidationCapture(server.Client(), &mcpOAuthCredentialAuth{}, mcpOAuthCredentialSecret{}, nil)
	capture.bearer = "synthetic-access"
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	response, err := capture.client.Do(req)
	if err != nil || response == nil || ctx.Err() != nil {
		t.Fatal("diagnostic buffered a live successful SSE stream")
	}
	_ = response.Body.Close()
}

func TestCredentialDiagnosticRefreshFailureIsRealAndRedacted(t *testing.T) {
	secret := "diagnostic-access/+<&>\"' :"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "diagnostic-refresh" {
			t.Error("missing real refresh grant")
		}
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(secret + html.EscapeString(secret) + url.QueryEscape(secret) + base64.StdEncoding.EncodeToString([]byte(secret)) + hex.EncodeToString([]byte(secret)) + strings.ToUpper(hex.EncodeToString([]byte(secret)))))
	}))
	defer server.Close()
	svc := newTestSecretsService(t)
	credential := sealedMCPOAuthCredential(t, svc, server.URL, secret, "diagnostic-refresh", nil)
	store := &fakeCredentialStore{get: credential}
	i := newTestInjector(t, svc, store, server.Client(), oauthRefreshNow())
	result, err := i.validateMCPOAuth(t.Context(), credential)
	if err != nil || calls.Load() != 1 || result.Status != "invalid" || result.Refresh.Status != "failed" || result.MCPProbe != nil || store.updateCalls != 0 {
		t.Fatalf("unexpected diagnostic outcome: %+v, err=%v calls=%d", result, err, calls.Load())
	}
	if result.Refresh.HTTPResponse == nil || result.Refresh.HTTPResponse.StatusCode != 400 || strings.Contains(result.Refresh.HTTPResponse.Body, "diagnostic-access") || !strings.Contains(result.Refresh.HTTPResponse.Body, "[REDACTED]") {
		t.Fatal("missing real sanitized downstream response")
	}
	for _, encoded := range []string{hex.EncodeToString([]byte(secret)), strings.ToUpper(hex.EncodeToString([]byte(secret)))} {
		if strings.Contains(result.Refresh.HTTPResponse.Body, encoded) {
			t.Fatal("hex-encoded secret escaped response redaction")
		}
	}
}

func TestCredentialDiagnosticWithoutRefreshPerformsMCP(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer diagnostic-access" {
					t.Error("missing bearer")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"diagnostic-access"}`))
			}))
			defer server.Close()
			svc := newTestSecretsService(t)
			auth := &mcpOAuthCredentialAuth{Type: credentialAuthTypeMCPOAuth, MCPServerURL: server.URL}
			credential := sealMCPOAuthCredential(t, svc, auth, mcpOAuthCredentialSecret{Type: credentialAuthTypeMCPOAuth, AccessToken: "diagnostic-access"}, "diagnostic-access")
			i := newTestInjector(t, svc, &fakeCredentialStore{}, server.Client(), oauthRefreshNow())
			result, err := i.validateMCPOAuth(t.Context(), credential)
			if err != nil || calls.Load() != 1 || result.Status != validationHTTPVerdict(status) || result.HasRefreshToken || result.Refresh.Status != "no_refresh_token" || result.MCPProbe == nil || result.MCPProbe.Method != "initialize" {
				t.Fatalf("unexpected outcome: %+v err=%v calls=%d", result, err, calls.Load())
			}
			if result.MCPProbe.HTTPResponse == nil || result.MCPProbe.HTTPResponse.Body != `{"error":"[REDACTED]"}` {
				t.Fatal("unsafe diagnostic body")
			}
		})
	}
}

func TestCredentialDiagnosticRefreshPersistsThenProbes(t *testing.T) {
	var tokenCalls, probes atomic.Int32
	mcpServer := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			tokenCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"new-diagnostic-token","refresh_token":"new-refresh-token","expires_in":3600}`))
			return
		}
		probes.Add(1)
		if r.Header.Get("Authorization") != "Bearer new-diagnostic-token" {
			t.Error("probe did not use refreshed token")
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	defer server.Close()
	svc := newTestSecretsService(t)
	auth := testMCPOAuthPublicAuth(server.URL+"/token", nil)
	auth.MCPServerURL = server.URL + "/mcp"
	credential := sealMCPOAuthCredential(t, svc, auth, testMCPOAuthSecret("old-diagnostic-token", "old-refresh-token"), "old-diagnostic-token")
	store := &fakeCredentialStore{get: credential}
	i := newTestInjector(t, svc, store, server.Client(), oauthRefreshNow())
	result, err := i.validateMCPOAuth(t.Context(), credential)
	if err != nil || result.Status != "valid" || result.Refresh.Status != "succeeded" || tokenCalls.Load() != 1 || probes.Load() == 0 || store.updateCalls != 1 {
		t.Fatalf("unexpected result %+v err=%v", result, err)
	}
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), "new-diagnostic-token") || strings.Contains(string(raw), "new-refresh-token") {
		t.Fatal("new tokens leaked")
	}
}
