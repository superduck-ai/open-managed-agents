package tests

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDirectoryServersCORSPreflight(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("directory-servers-preflight-bucket"))
	defer app.close()

	req, err := http.NewRequest(http.MethodOptions, app.baseURL+"/api/directory/servers?type=remote&visibility=commercial&sort=popular&limit=500", nil)
	if err != nil {
		t.Fatalf("new directory preflight request: %v", err)
	}
	req.Host = "api.anthropic.com"
	req.Header.Set("Origin", "https://platform.claude.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "anthropic-client-version,content-type")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do directory preflight request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("directory preflight status = %d, want 204: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://platform.claude.com" {
		t.Fatalf("preflight allow origin = %q, want platform origin", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("preflight allow credentials = %q, want true", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Headers"); got != "anthropic-client-version,content-type" {
		t.Fatalf("preflight allow headers = %q, want requested headers", got)
	}
	if got := resp.Header.Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("preflight allow private network = %q, want true", got)
	}
}

func TestDirectoryServersRoute(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("directory-servers-bucket"))
	defer app.close()

	req, err := http.NewRequest(http.MethodGet, app.baseURL+"/api/directory/servers?type=remote&visibility=commercial&sort=popular&limit=500", nil)
	if err != nil {
		t.Fatalf("new directory request: %v", err)
	}
	req.Host = "api.anthropic.com"
	req.Header.Set("Origin", "https://platform.claude.com")
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do directory request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("directory status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("directory content-type = %q, want json", ct)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://platform.claude.com" {
		t.Fatalf("directory allow origin = %q, want platform origin", got)
	}
	var body struct {
		Servers []map[string]any `json:"servers"`
	}
	decodeJSON(t, resp.Body, &body)
	if len(body.Servers) == 0 {
		t.Fatal("directory servers empty, want source fixture data")
	}
	if body.Servers[0]["type"] != "remote" || body.Servers[0]["name"] == "" {
		t.Fatalf("first directory server = %#v, want remote server", body.Servers[0])
	}
}

func TestPlatformOrganizationProxyMessagesRequiresConfiguredModel(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("platform-proxy-provider-errors-bucket"))
	defer app.close()
	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "proxy-provider-errors@example.com")
	path := "/api/organizations/" + orgUUID + "/proxy/v1/messages"

	unknown := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"model":"not-configured","max_tokens":16,"messages":[]}`), cookies)
	defer unknown.Body.Close()
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown model status = %d, want 400: %s", unknown.StatusCode, readAll(t, unknown.Body))
	}

	clearTestLLMProviders(t, app)
	missing := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"model":"kimi-k2.5","max_tokens":16,"messages":[]}`), cookies)
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("missing Provider status = %d, want 503: %s", missing.StatusCode, readAll(t, missing.Body))
	}
}

func TestPlatformOrganizationProxyMessagesForwardsOnlyProtocolHeaders(t *testing.T) {
	upstreamHeaders := make(chan http.Header, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders <- r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()

	app := newTestAppWithStore(t, nil, newFakeStore("platform-proxy-headers-bucket"))
	defer app.close()
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Proxy test", upstream.URL, "server-provider-key", "kimi-k2.5")

	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "proxy-headers@example.com")
	response := app.platformRequestWithHeaders(
		t,
		http.MethodPost,
		"/api/organizations/"+orgUUID+"/proxy/v1/messages",
		strings.NewReader(`{"model":"kimi-k2.5","stream":true,"max_tokens":16,"messages":[]}`),
		cookies,
		map[string]string{
			"Accept":            "text/event-stream",
			"Anthropic-Version": "2023-06-01",
			"Anthropic-Beta":    "test-beta",
			"X-CSRF-Token":      "browser-csrf",
			"Connection":        "X-Connection-Only",
			"X-Connection-Only": "browser-connection-secret",
		},
	)
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("proxy status = %d, want 202: %s", response.StatusCode, readAll(t, response.Body))
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "data: ok\n\n" {
		t.Fatalf("proxy stream = %q, %v", body, err)
	}

	headers := <-upstreamHeaders
	if headers.Get("X-API-Key") != "server-provider-key" ||
		headers.Get("Authorization") != "Bearer server-provider-key" ||
		headers.Get("Accept") != "text/event-stream" ||
		headers.Get("Content-Type") != "application/json" ||
		headers.Get("Anthropic-Version") != "2023-06-01" ||
		headers.Get("Anthropic-Beta") != "test-beta" {
		t.Fatalf("protocol headers = %#v", headers)
	}
	for _, name := range []string{
		"Cookie",
		"X-CSRF-Token",
		"X-Organization-UUID",
		"X-Workspace-ID",
		"Connection",
		"X-Connection-Only",
	} {
		if value := headers.Get(name); value != "" {
			t.Fatalf("sensitive header %s reached upstream: %q", name, value)
		}
	}
}
