package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
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

func TestPlatformOrganizationProxyMessages(t *testing.T) {
	var (
		mu       sync.Mutex
		seenPath string
		seenAuth string
		seenKey  string
		seenBody string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		mu.Lock()
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenKey = r.Header.Get("X-API-Key")
		seenBody = string(body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_proxy_test","type":"message"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "sk-ant-upstream-proxy-test"
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-proxy-messages-bucket"))
	defer app.close()

	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "proxy-messages@example.com")
	payload := `{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/api/organizations/"+orgUUID+"/proxy/v1/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new proxy request: %v", err)
	}
	req.Host = "platform.claude.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer browser-oauth-token")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do proxy request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var response map[string]any
	decodeJSON(t, resp.Body, &response)
	if response["id"] != "msg_proxy_test" {
		t.Fatalf("proxy response = %#v, want upstream response", response)
	}

	mu.Lock()
	defer mu.Unlock()
	if seenPath != "/v1/messages" {
		t.Fatalf("upstream path = %s, want /v1/messages", seenPath)
	}
	if seenAuth != "" {
		t.Fatalf("upstream authorization = %q, want stripped", seenAuth)
	}
	if seenKey != "sk-ant-upstream-proxy-test" {
		t.Fatalf("upstream x-api-key = %q, want configured key", seenKey)
	}
	if !json.Valid([]byte(seenBody)) || seenBody != payload {
		t.Fatalf("upstream body = %q, want original JSON body", seenBody)
	}
}

func TestPlatformOrganizationProxyMessagesMapsConfiguredModel(t *testing.T) {
	var seenBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		seenBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_proxy_mapped_model","type":"message"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "sk-ant-upstream-model-mapping-test"
	cfg.AnthropicUpstream.ModelMappings = map[string]string{
		"claude-sonnet-4-6": "glm-5-turbo",
	}
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-proxy-model-mapping-bucket"))
	defer app.close()

	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "proxy-model-mapping@example.com")
	payload := `{"model":"claude-sonnet-4-6","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/api/organizations/"+orgUUID+"/proxy/v1/messages", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new proxy request: %v", err)
	}
	req.Host = "platform.claude.com"
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do proxy request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}

	var upstreamBody struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(seenBody, &upstreamBody); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	if upstreamBody.Model != "glm-5-turbo" {
		t.Fatalf("upstream model = %v, want glm-5-turbo", upstreamBody.Model)
	}
	if upstreamBody.MaxTokens != 16 || len(upstreamBody.Messages) != 1 {
		t.Fatalf("upstream body lost request fields: %#v", upstreamBody)
	}
}

func TestPlatformOrganizationProxyMessagesStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\"}\n\n"))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.AnthropicUpstream.BaseURL = upstream.URL
	cfg.AnthropicUpstream.APIKey = "sk-ant-upstream-proxy-stream-test"
	app := newTestAppWithStore(t, &cfg, newFakeStore("platform-proxy-messages-stream-bucket"))
	defer app.close()

	orgUUID := loadDefaultOrganizationUUID(t, app)
	cookies := app.platformLoginCookies(t, "proxy-messages-stream@example.com")
	req, err := http.NewRequest(http.MethodPost, app.baseURL+"/api/organizations/"+orgUUID+"/proxy/v1/messages", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("new proxy stream request: %v", err)
	}
	req.Host = "platform.claude.com"
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do proxy stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("proxy stream status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("proxy stream content-type = %q, want event stream", ct)
	}
	if cache := resp.Header.Get("Cache-Control"); cache != "no-cache" {
		t.Fatalf("proxy stream cache-control = %q, want no-cache", cache)
	}
	body := string(readAll(t, resp.Body))
	if !strings.Contains(body, "message_start") {
		t.Fatalf("proxy stream body = %q, want upstream SSE", body)
	}
}
