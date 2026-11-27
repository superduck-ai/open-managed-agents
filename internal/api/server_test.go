package api

import (
	"bytes"

	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"

	"github.com/superduck-ai/open-managed-agents/internal/filestore"
)

func TestNewServerUsesDefaultLoggerForHTTPAccess(t *testing.T) {
	var logs bytes.Buffer
	previousDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		slog.SetDefault(previousDefault)
	})

	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create code session credentials: %v", err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatalf("create filestore credentials: %v", err)
	}
	server := NewServer(ServerDeps{
		CodeSessionCredentials: credentials,
		FilestoreCredentials:   filestoreCredentials,
	})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	entries := parseSlogJSONLines(t, logs.String())
	if len(entries) != 2 {
		t.Fatalf("access log entries = %d, want 2: %s", len(entries), logs.String())
	}
	if entries[0]["component"] != "http" || entries[0]["msg"] != "http request" {
		t.Fatalf("unexpected request access log: %#v", entries[0])
	}
	if entries[1]["component"] != "http" || entries[1]["msg"] != "http response" {
		t.Fatalf("unexpected response access log: %#v", entries[1])
	}
}

func TestNewServerLoadsOptionalPricingConfig(t *testing.T) {
	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		content string
		missing bool
		warn    bool
		cost    bool
	}{
		{name: "未配置"},
		{name: "文件缺失", missing: true, warn: true},
		{name: "未知配置", content: "database: {}\n", warn: true},
		{name: "规则无效", content: "pricing:\n  response_costs:\n    example: {}\n", warn: true},
		{name: "规则生效", cost: true, content: `pricing:
  response_costs:
    example:
      currency: CNY
      exchange_rate: "7.2"
      protocols:
        messages:
          non_streaming:
            amount_pointer: /receipt/amount
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			if tc.missing || tc.content != "" {
				path = filepath.Join(t.TempDir(), "optional.yaml")
			}
			if tc.content != "" {
				if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var logs bytes.Buffer
			server := NewServer(ServerDeps{
				Config:                 config.Config{OptionalConfigFile: path},
				Logger:                 slog.New(slog.NewTextHandler(&logs, nil)),
				CodeSessionCredentials: credentials,
				FilestoreCredentials:   filestoreCredentials,
			})
			rule := server.billing.ResponseCost("example/model")
			cost := rule.ExtractFor("messages", false, []byte(`{"receipt":{"amount":"7.2"}}`))
			if (cost != nil) != tc.cost || cost != nil && cost.USDCents != 100 {
				t.Fatalf("费用提取结果不符：%+v", cost)
			}
			if bytes.Contains(logs.Bytes(), []byte("可选配置未加载")) != tc.warn {
				t.Fatalf("可选配置警告不符：%s", logs.String())
			}
		})
	}
}

func TestPlatformCSRFMiddlewareProtectsUnsafeRequests(t *testing.T) {
	t.Parallel()
	called := false
	handler := platformCSRFMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	missing := httptest.NewRequest(http.MethodPost, "/api/console/organizations/org", nil)
	missing.AddCookie(&http.Cookie{Name: "sessionKey", Value: "session-secret"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missing)
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("missing CSRF response = %d, called = %v", response.Code, called)
	}

	valid := httptest.NewRequest(http.MethodPost, "/api/console/organizations/org", nil)
	valid.AddCookie(&http.Cookie{Name: "sessionKey", Value: "session-secret"})
	valid.Header.Set("X-CSRF-Token", auth.PlatformCSRFToken("session-secret"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	if response.Code != http.StatusOK || !called {
		t.Fatalf("valid CSRF response = %d, called = %v", response.Code, called)
	}
}

func TestPlatformSessionCSRFOnIdentityRoutes(t *testing.T) {
	t.Parallel()
	const sessionKey = "identity-session"
	handler := platformSessionCSRF(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		name, method, credential, token string
		status                          int
	}{
		{"Cookie 写请求缺少令牌", http.MethodPost, auth.CredentialTypePlatformSession, "", http.StatusForbidden},
		{"Cookie 解绑令牌不匹配", http.MethodDelete, auth.CredentialTypePlatformSession, auth.PlatformCSRFToken("other"), http.StatusForbidden},
		{"Cookie 合法写请求", http.MethodPost, auth.CredentialTypePlatformSession, auth.PlatformCSRFToken(sessionKey), http.StatusNoContent},
		{"Cookie 读取", http.MethodGet, auth.CredentialTypePlatformSession, "", http.StatusNoContent},
		{"API Key 写请求", http.MethodPost, auth.CredentialTypeAPIKey, "", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "/v1/identities/idn_test/enable", nil)
			request.AddCookie(&http.Cookie{Name: "sessionKey", Value: sessionKey})
			request.Header.Set("X-CSRF-Token", tc.token)
			request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{CredentialType: tc.credential}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("状态=%d，预期=%d", response.Code, tc.status)
			}
		})
	}
}

func TestReadinessReportsMissingDependencies(t *testing.T) {
	t.Parallel()
	server := &Server{}
	response := httptest.NewRecorder()
	server.handleReadiness(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if body := response.Body.String(); !bytes.Contains([]byte(body), []byte(`"database":"unavailable"`)) || !bytes.Contains([]byte(body), []byte(`"tunnel_nats":"unavailable"`)) || !bytes.Contains([]byte(body), []byte(`"tunnel_redis":"unavailable"`)) {
		t.Fatalf("readiness body = %s", body)
	}
}
