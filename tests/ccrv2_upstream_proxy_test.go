package tests

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"golang.org/x/net/websocket"
)

type capturedRuntimeModelRequest struct {
	path          string
	apiKey        string
	authorization string
	version       string
	body          string
	contentLength int64
}

type repeatedRuntimeModelByteReader struct{}

func (repeatedRuntimeModelByteReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestCCRV2RuntimeEndpoints(t *testing.T) {
	upstreamRequests := make(chan capturedRuntimeModelRequest, 1)
	upstreamStartedReading := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Header.Get("X-Test-Streaming") == "true" {
			firstByte := make([]byte, 1)
			if _, err := io.ReadFull(r.Body, firstByte); err == nil {
				upstreamStartedReading <- struct{}{}
			}
			rest, _ := io.ReadAll(r.Body)
			body = append(body, firstByte...)
			body = append(body, rest...)
		} else {
			body, _ = io.ReadAll(r.Body)
		}
		upstreamRequests <- capturedRuntimeModelRequest{
			path:          r.URL.Path,
			apiKey:        r.Header.Get("X-Api-Key"),
			authorization: r.Header.Get("Authorization"),
			version:       r.Header.Get("anthropic-version"),
			body:          string(body),
			contentLength: r.ContentLength,
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Test", "reached")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// 真实上游配置只存在于 API server；测试通过捕获请求验证 sandbox token 会被替换。
	cfg.AnthropicUpstream.BaseURL = upstream.URL + "/coding"
	cfg.AnthropicUpstream.APIKey = "upstream-secret"
	app := newTestAppWithStore(t, &cfg, newFakeStore("ccrv2-upstream-proxy-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"ccrv2-upstream-proxy-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	environment := createEnvironment(t, app, `{"name":"ccrv2-upstream-proxy-env"}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	messagesToken, err := ids.New("sk-ant-oat01-ccrv2-")
	if err != nil {
		t.Fatalf("generate code session Messages token: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update code_sessions
		set oauth_access_token_hash = $2
		where external_id = $1
	`, codeSessionID, auth.HashAPIKey(messagesToken)); err != nil {
		t.Fatalf("set code session Messages token: %v", err)
	}
	sessionIngressToken := codeSessionIngressToken(t, app, codeSessionID)
	if !strings.HasPrefix(sessionIngressToken, "sk-ant-si-") {
		t.Fatalf("unexpected session ingress credential: %q", sessionIngressToken)
	}

	t.Run("failure model proxy requires code session token", func(t *testing.T) {
		response := postRuntimeModelRequest(t, app, "", "")
		assertError(t, response, http.StatusUnauthorized, "authentication_error")

		response = postRuntimeModelRequest(t, app, "invalid-code-session", "")
		assertError(t, response, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure websocket requires code session token", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/code/upstreamproxy/ws", nil)
		if err != nil {
			t.Fatalf("new websocket probe request: %v", err)
		}
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatalf("websocket probe request: %v", err)
		}
		assertError(t, response, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure websocket blocks private CONNECT targets", func(t *testing.T) {
		// 默认配置必须拒绝 loopback，即使 WebSocket Bearer 和 CONNECT Basic 都合法。
		connection := dialCCRV2UpstreamProxy(t, app, sessionIngressToken)
		defer connection.Close()
		authorization := base64.StdEncoding.EncodeToString([]byte(codeSessionID + ":" + sessionIngressToken))
		connectHead := "CONNECT 127.0.0.1:443 HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
		if err := websocket.Message.Send(connection, encodeCCRV2TestChunk([]byte(connectHead))); err != nil {
			t.Fatalf("send private CONNECT request: %v", err)
		}
		status := receiveCCRV2TestChunk(t, connection)
		if !strings.HasPrefix(string(status), "HTTP/1.1 403 Forbidden") {
			t.Fatalf("private CONNECT status = %q", status)
		}
	})

	t.Run("failure websocket requires matching Basic token", func(t *testing.T) {
		connection := dialCCRV2UpstreamProxy(t, app, sessionIngressToken)
		defer connection.Close()
		authorization := base64.StdEncoding.EncodeToString([]byte(codeSessionID + ":sk-ant-si-other"))
		connectHead := "CONNECT 1.1.1.1:443 HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
		if err := websocket.Message.Send(connection, encodeCCRV2TestChunk([]byte(connectHead))); err != nil {
			t.Fatalf("send mismatched CONNECT request: %v", err)
		}
		status := receiveCCRV2TestChunk(t, connection)
		if !strings.HasPrefix(string(status), "HTTP/1.1 407 Proxy Authentication Required") {
			t.Fatalf("mismatched CONNECT status = %q", status)
		}
	})

	// Environment networking 由 proxy 在每次 CONNECT 时实时解析。本 app 默认开启 SSRF
	// 保护，这里只覆盖"未授权 host 在拨号前被策略 403"；授权放行与实时收紧的
	// 正向证据见 TestCCRV2UpstreamProxyNetworkPolicyAllow（SSRF 关闭的独立 app，
	// 避免本机 fake-IP DNS 让两个路径返回相同的 403）。
	limitedEnvironment := createEnvironment(t, app, `{"name":"ccrv2-upstream-proxy-limited-env","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":["nonexistent.invalid"],"allow_mcp_servers":false,"allow_package_managers":false}}}`)
	defer cleanupEnvironmentRows(t, app.db, limitedEnvironment.ID)
	limitedSession := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(limitedEnvironment.ID)+`}`)
	limitedCodeSessionID := launchLocalCodeSession(t, app, limitedSession.ID)
	limitedIngressToken := codeSessionIngressToken(t, app, limitedCodeSessionID)

	t.Run("failure limited environment denies unlisted host before dial", func(t *testing.T) {
		status := connectViaCCRV2Proxy(t, app, limitedCodeSessionID, limitedIngressToken, "1.1.1.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("unlisted CONNECT status = %q, want 403", status)
		}
	})

	// 即使 allowed_hosts 显式列出私网/loopback 地址，策略放行后仍必须被 SSRF
	// 地址过滤拦截：策略模块不替代 proxy 的地址检查（两层独立 403）。
	listedPrivateEnvironment := createEnvironment(t, app, `{"name":"ccrv2-upstream-proxy-listed-private-env","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":["127.0.0.1"],"allow_mcp_servers":false,"allow_package_managers":false}}}`)
	defer cleanupEnvironmentRows(t, app.db, listedPrivateEnvironment.ID)
	listedPrivateSession := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(listedPrivateEnvironment.ID)+`}`)
	listedPrivateCodeSessionID := launchLocalCodeSession(t, app, listedPrivateSession.ID)
	listedPrivateIngressToken := codeSessionIngressToken(t, app, listedPrivateCodeSessionID)

	t.Run("failure listed private host still blocked by SSRF protection", func(t *testing.T) {
		status := connectViaCCRV2Proxy(t, app, listedPrivateCodeSessionID, listedPrivateIngressToken, "127.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("listed private CONNECT status = %q, want 403 from SSRF protection", status)
		}
	})
	registerCodeSessionWorker(t, app, codeSessionID)

	t.Run("failure model proxy rejects declared oversized body", func(t *testing.T) {
		const declaredSize = (32 << 20) + 1
		body := io.LimitReader(repeatedRuntimeModelByteReader{}, declaredSize)
		request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", body)
		if err != nil {
			t.Fatalf("new oversized model request: %v", err)
		}
		request.ContentLength = declaredSize
		request.Header.Set("X-Api-Key", messagesToken)
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatalf("oversized model request: %v", err)
		}
		assertError(t, response, http.StatusRequestEntityTooLarge, "request_too_large")
	})

	t.Run("success CA certificate is publicly downloadable", func(t *testing.T) {
		response, err := app.client.Get(app.baseURL + "/v1/code/upstreamproxy/ca-cert")
		if err != nil {
			t.Fatalf("download CA certificate: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("CA status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
		}
		certificatePEM, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read CA certificate: %v", err)
		}
		block, rest := pem.Decode(certificatePEM)
		if block == nil || len(rest) != 0 {
			t.Fatalf("invalid CA PEM: block=%v rest=%q", block, rest)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse CA certificate: %v", err)
		}
		if !certificate.IsCA {
			t.Fatal("downloaded certificate is not a CA")
		}
	})

	t.Run("success model proxy streams request body", func(t *testing.T) {
		firstChunk := strings.Repeat("x", 64<<10)
		secondChunk := "tail"
		bodyReader, bodyWriter := io.Pipe()
		writeResult := make(chan error, 1)
		go func() {
			if _, err := bodyWriter.Write([]byte(firstChunk)); err != nil {
				writeResult <- err
				return
			}
			select {
			case <-upstreamStartedReading:
				_, err := bodyWriter.Write([]byte(secondChunk))
				if closeErr := bodyWriter.Close(); err == nil {
					err = closeErr
				}
				writeResult <- err
			case <-time.After(3 * time.Second):
				err := errors.New("upstream did not start reading streamed request body")
				_ = bodyWriter.CloseWithError(err)
				writeResult <- err
			}
		}()

		request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", bodyReader)
		if err != nil {
			t.Fatalf("new streaming model request: %v", err)
		}
		request.ContentLength = int64(len(firstChunk) + len(secondChunk))
		request.Header.Set("X-Api-Key", messagesToken)
		request.Header.Set("X-Test-Streaming", "true")
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatalf("streaming model request: %v", err)
		}
		defer response.Body.Close()
		if err := <-writeResult; err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("streaming model proxy status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
		}
		captured := <-upstreamRequests
		if captured.body != firstChunk+secondChunk || captured.contentLength != request.ContentLength {
			t.Fatalf("unexpected streamed request: body bytes=%d content-length=%d", len(captured.body), captured.contentLength)
		}
	})

	t.Run("success model proxy keeps upstream secret server-side", func(t *testing.T) {
		body := `{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
		response := postRuntimeModelRequest(t, app, messagesToken, body)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("model proxy status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
		}
		if response.Header.Get("X-Upstream-Test") != "reached" {
			t.Fatalf("missing upstream response header: %#v", response.Header)
		}
		captured := <-upstreamRequests
		if captured.path != "/coding/v1/messages" || captured.apiKey != "upstream-secret" {
			t.Fatalf("unexpected upstream target/auth: %+v", captured)
		}
		if captured.authorization != "" || captured.version != "2023-06-01" || captured.body != body || captured.contentLength != int64(len(body)) {
			t.Fatalf("unexpected forwarded request: %+v", captured)
		}
	})
}

// TestCCRV2UpstreamProxyPolicyChainFailures 覆盖策略解析链的失败语义：
// Code Session 只读取自己绑定的 Environment（不能借用同 workspace 其他
// Environment 的 allowlist），且租户作用域不匹配或绑定资源缺失时 fail closed。
// 与上面相同使用 SSRF 关闭的 app：403 只能来自策略授权。
func TestCCRV2UpstreamProxyPolicyChainFailures(t *testing.T) {
	app := newSSRFDisabledTestApp(t, "ccrv2-network-policy-chain-bucket")
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"ccrv2-network-policy-chain-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	boundEnvironment := createEnvironment(t, app, `{"name":"ccrv2-network-policy-bound-env","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}}`)
	defer cleanupEnvironmentRows(t, app.db, boundEnvironment.ID)
	otherEnvironment := createEnvironment(t, app, `{"name":"ccrv2-network-policy-other-env","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":["10.0.0.1"],"allow_mcp_servers":false,"allow_package_managers":false}}}`)
	defer cleanupEnvironmentRows(t, app.db, otherEnvironment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(boundEnvironment.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	ingressToken := codeSessionIngressToken(t, app, codeSessionID)
	connect := func(t *testing.T, target string) string {
		t.Helper()
		return connectViaCCRV2Proxy(t, app, codeSessionID, ingressToken, target)
	}
	setBoundHostAllowed := func(t *testing.T, allowed bool) {
		t.Helper()
		config := `{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}`
		if allowed {
			config = `{"type":"cloud","networking":{"type":"limited","allowed_hosts":["10.0.0.1"],"allow_mcp_servers":false,"allow_package_managers":false}}`
		}
		if _, err := app.db.Pool.Exec(context.Background(), `
			update environments
			set config = $2::jsonb
			where external_id = $1
		`, boundEnvironment.ID, config); err != nil {
			t.Fatalf("update bound environment allowlist: %v", err)
		}
	}

	t.Run("failure code session cannot borrow another environment allowlist", func(t *testing.T) {
		// 10.0.0.1 只在 otherEnvironment 的 allowlist 里；若被借用，SSRF 关闭时
		// 拨号会失败为 502，正确的策略隔离必须返回 403。
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("cross-environment CONNECT status = %q, want 403", status)
		}
	})

	t.Run("failure signed token with mismatched workspace scope fails closed", func(t *testing.T) {
		mismatchedToken := codeSessionIngressTokenWithScope(
			t,
			app,
			codeSessionID,
			"",
			"00000000-0000-0000-0000-000000000099",
		)
		status := connectViaCCRV2Proxy(t, app, codeSessionID, mismatchedToken, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("mismatched-workspace CONNECT status = %q, want 403", status)
		}
	})

	t.Run("failure signed token with mismatched organization scope fails closed", func(t *testing.T) {
		mismatchedToken := codeSessionIngressTokenWithScope(
			t,
			app,
			codeSessionID,
			"00000000-0000-0000-0000-000000000098",
			"",
		)
		status := connectViaCCRV2Proxy(t, app, codeSessionID, mismatchedToken, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("mismatched-organization CONNECT status = %q, want 403", status)
		}
	})

	t.Run("failure malformed persisted allowlist fails closed as a whole", func(t *testing.T) {
		if _, err := app.db.Pool.Exec(context.Background(), `
			update environments
			set config = '{"type":"cloud","networking":{"type":"limited","allowed_hosts":["bad/path","10.0.0.1"],"allow_mcp_servers":false,"allow_package_managers":false}}'::jsonb
			where external_id = $1
		`, boundEnvironment.ID); err != nil {
			t.Fatalf("persist malformed environment policy: %v", err)
		}
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("malformed-policy CONNECT status = %q, want 403", status)
		}
		if _, err := app.db.Pool.Exec(context.Background(), `
			update environments
			set config = '{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}'::jsonb
			where external_id = $1
		`, boundEnvironment.ID); err != nil {
			t.Fatalf("restore environment policy: %v", err)
		}
	})

	t.Run("failure malformed MCP contract fails closed before explicit host matching", func(t *testing.T) {
		codeSession, err := app.db.GetCodeSession(context.Background(), codeSessionID)
		if err != nil {
			t.Fatalf("load code session scope: %v", err)
		}
		originalSession, err := app.db.GetSession(context.Background(), codeSession.WorkspaceID, session.ID)
		if err != nil {
			t.Fatalf("load original session snapshot: %v", err)
		}
		if _, err := app.db.Pool.Exec(context.Background(), `
			update environments
			set config = '{"type":"cloud","networking":{"type":"limited","allowed_hosts":["10.0.0.1"],"allow_mcp_servers":true,"allow_package_managers":false}}'::jsonb
			where external_id = $1
		`, boundEnvironment.ID); err != nil {
			t.Fatalf("enable MCP policy: %v", err)
		}
		for _, snapshot := range []string{
			`{"mcp_servers":[{"type":"http","url":"://bad"}]}`,
			`{"mcp_servers":[{"url":"https://evil.example/mcp"}]}`,
			`{"mcp_servers":[{"type":"stdio","url":"https://evil.example/mcp"}]}`,
			`{"mcp_servers":[{"type":"url","url":"ftp://evil.example/mcp"}]}`,
		} {
			if _, err := app.db.Pool.Exec(context.Background(), `
				update sessions set agent_snapshot = $2::jsonb where external_id = $1
			`, session.ID, snapshot); err != nil {
				t.Fatalf("persist malformed MCP contract %s: %v", snapshot, err)
			}
			status := connect(t, "10.0.0.1:443")
			if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
				t.Fatalf("malformed-MCP CONNECT status = %q, want 403 for %s", status, snapshot)
			}
		}
		if _, err := app.db.Pool.Exec(context.Background(), `
			update sessions set agent_snapshot = $2::jsonb where external_id = $1
		`, session.ID, originalSession.AgentSnapshot); err != nil {
			t.Fatalf("restore session snapshot: %v", err)
		}
		if _, err := app.db.Pool.Exec(context.Background(), `
			update environments
			set config = '{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}'::jsonb
			where external_id = $1
		`, boundEnvironment.ID); err != nil {
			t.Fatalf("restore environment policy: %v", err)
		}
	})

	t.Run("failure missing session row fails closed", func(t *testing.T) {
		if _, err := app.db.Pool.Exec(context.Background(), `update sessions set deleted_at = now() where external_id = $1`, session.ID); err != nil {
			t.Fatalf("soft-delete bound session: %v", err)
		}
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("missing-session CONNECT status = %q, want 403", status)
		}
		if _, err := app.db.Pool.Exec(context.Background(), `update sessions set deleted_at = null where external_id = $1`, session.ID); err != nil {
			t.Fatalf("restore bound session: %v", err)
		}
	})

	t.Run("failure inactive code session fails closed", func(t *testing.T) {
		setBoundHostAllowed(t, true)
		t.Cleanup(func() { setBoundHostAllowed(t, false) })
		if _, err := app.db.Pool.Exec(context.Background(), `update code_sessions set status = 'stopped' where external_id = $1`, codeSessionID); err != nil {
			t.Fatalf("deactivate code session: %v", err)
		}
		t.Cleanup(func() {
			if _, err := app.db.Pool.Exec(context.Background(), `update code_sessions set status = 'active' where external_id = $1`, codeSessionID); err != nil {
				t.Errorf("restore code session status: %v", err)
			}
		})
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("inactive-code-session CONNECT status = %q, want 403", status)
		}
	})

	t.Run("failure terminated session fails closed", func(t *testing.T) {
		setBoundHostAllowed(t, true)
		t.Cleanup(func() { setBoundHostAllowed(t, false) })
		if _, err := app.db.Pool.Exec(context.Background(), `update sessions set status = 'terminated' where external_id = $1`, session.ID); err != nil {
			t.Fatalf("terminate bound session: %v", err)
		}
		t.Cleanup(func() {
			if _, err := app.db.Pool.Exec(context.Background(), `update sessions set status = 'idle' where external_id = $1`, session.ID); err != nil {
				t.Errorf("restore session status: %v", err)
			}
		})
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("terminated-session CONNECT status = %q, want 403", status)
		}
	})

	t.Run("failure missing environment row fails closed", func(t *testing.T) {
		if _, err := app.db.Pool.Exec(context.Background(), `delete from environments where external_id = $1`, boundEnvironment.ID); err != nil {
			t.Fatalf("delete bound environment: %v", err)
		}
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("missing-environment CONNECT status = %q, want 403", status)
		}
	})
}

// TestCCRV2UpstreamProxyNetworkPolicyAllow 在关闭 SSRF 地址过滤的 app 上验证策略放行路径：
// 授权 host 通过策略后进入拨号（本机 443 有监听时为 200，否则为 502），收紧配置后
// 同一目标变成策略 403。SSRF 关闭时 403 只能来自策略授权，与地址过滤的 403 无歧义。
func TestCCRV2UpstreamProxyNetworkPolicyAllow(t *testing.T) {
	app := newSSRFDisabledTestApp(t, "ccrv2-network-policy-allow-bucket")
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"ccrv2-network-policy-allow-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	environmentName, err := ids.New("ccrv2-network-policy-allow-env-")
	if err != nil {
		t.Fatalf("generate environment name: %v", err)
	}
	environment := createEnvironment(t, app, `{"name":`+quoteJSON(environmentName)+`,"config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":["127.0.0.1"],"allow_mcp_servers":false,"allow_package_managers":false}}}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	ingressToken := codeSessionIngressToken(t, app, codeSessionID)
	connect := func(t *testing.T, target string) string {
		t.Helper()
		return connectViaCCRV2Proxy(t, app, codeSessionID, ingressToken, target)
	}

	t.Run("failure unlisted host denied even with SSRF protection disabled", func(t *testing.T) {
		status := connect(t, "10.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("unlisted CONNECT status = %q, want 403", status)
		}
	})

	t.Run("success listed host reaches dial before live policy tightening", func(t *testing.T) {
		status := connect(t, "127.0.0.1:443")
		dialSucceeded := strings.HasPrefix(status, "HTTP/1.1 200 Connection Established")
		dialRefused := strings.HasPrefix(status, "HTTP/1.1 502 Bad Gateway")
		if !dialSucceeded && !dialRefused {
			t.Fatalf("listed CONNECT status = %q, want 200 or 502 after policy allows dialing", status)
		}
		updateEnvironment(t, app, environment.ID, `{"config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}}`, http.StatusOK)
		status = connect(t, "127.0.0.1:443")
		if !strings.HasPrefix(status, "HTTP/1.1 403 Forbidden") {
			t.Fatalf("post-update CONNECT status = %q, want 403", status)
		}
	})
}

func codeSessionIngressTokenWithScope(
	t *testing.T,
	app *testApp,
	codeSessionID string,
	organizationUUID string,
	workspaceUUID string,
) string {
	t.Helper()
	record, err := app.db.GetCodeSession(context.Background(), codeSessionID)
	if err != nil {
		t.Fatalf("get code session: %v", err)
	}
	credentialContext, err := app.db.GetCodeSessionCredentialContextForIssue(
		context.Background(),
		record.OrganizationID,
		record.WorkspaceID,
		codeSessionID,
	)
	if err != nil {
		t.Fatalf("get code session credential context: %v", err)
	}
	if organizationUUID == "" {
		organizationUUID = credentialContext.OrganizationUUID
	}
	if workspaceUUID == "" {
		workspaceUUID = credentialContext.WorkspaceUUID
	}
	token, err := app.credentials.Issue(codesessions.SessionCredentialIdentity{
		SessionID:        credentialContext.CodeSessionExternalID,
		PublicSessionID:  credentialContext.PublicSessionExternalID,
		AgentID:          credentialContext.AgentExternalID,
		AgentVersion:     credentialContext.AgentVersion,
		OrganizationUUID: organizationUUID,
		WorkspaceUUID:    workspaceUUID,
		AccountEmail:     credentialContext.AccountEmail,
	})
	if err != nil {
		t.Fatalf("issue mismatched-scope token: %v", err)
	}
	return token
}

func postRuntimeModelRequest(t *testing.T, app *testApp, messagesToken string, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new runtime model request: %v", err)
	}
	if messagesToken != "" {
		request.Header.Set("X-Api-Key", messagesToken)
		request.Header.Set("Authorization", "Bearer "+messagesToken)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatalf("runtime model request: %v", err)
	}
	return response
}

// newSSRFDisabledTestApp 启动一个关闭 SSRF 地址过滤的 app：此时 403 只能来自
// 网络策略授权，与地址过滤的 403 无歧义（本机 fake-IP DNS 会把不存在域名解析进
// SSRF 阻断段，无法在主 app 上区分两个路径）。
func newSSRFDisabledTestApp(t *testing.T, bucket string) *testApp {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CodeSession.UpstreamProxyDisableSSRFProtection = true
	return newTestAppWithStore(t, &cfg, newFakeStore(bucket))
}

// connectViaCCRV2Proxy 经 upstream proxy WebSocket 隧道发送一个 CONNECT 头，
// 返回 framed HTTP 状态行。
func connectViaCCRV2Proxy(t *testing.T, app *testApp, codeSessionID string, ingressToken string, target string) string {
	t.Helper()
	connection := dialCCRV2UpstreamProxy(t, app, ingressToken)
	defer connection.Close()
	authorization := base64.StdEncoding.EncodeToString([]byte(codeSessionID + ":" + ingressToken))
	connectHead := "CONNECT " + target + " HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
	if err := websocket.Message.Send(connection, encodeCCRV2TestChunk([]byte(connectHead))); err != nil {
		t.Fatalf("send CONNECT %s: %v", target, err)
	}
	return string(receiveCCRV2TestChunk(t, connection))
}

func dialCCRV2UpstreamProxy(t *testing.T, app *testApp, sessionIngressToken string) *websocket.Conn {
	t.Helper()
	location := "ws" + strings.TrimPrefix(app.baseURL, "http") + "/v1/code/upstreamproxy/ws"
	config, err := websocket.NewConfig(location, app.baseURL)
	if err != nil {
		t.Fatalf("new upstream proxy websocket config: %v", err)
	}
	config.Header.Set("Authorization", "Bearer "+sessionIngressToken)
	config.Header.Set("Content-Type", "application/proto")
	connection, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("dial upstream proxy websocket: %v", err)
	}
	return connection
}

func encodeCCRV2TestChunk(data []byte) []byte {
	length := len(data)
	encoded := []byte{0x0a}
	for length >= 0x80 {
		encoded = append(encoded, byte(length)|0x80)
		length >>= 7
	}
	encoded = append(encoded, byte(length))
	return append(encoded, data...)
}

func receiveCCRV2TestChunk(t *testing.T, connection *websocket.Conn) []byte {
	t.Helper()
	var encoded []byte
	if err := websocket.Message.Receive(connection, &encoded); err != nil {
		t.Fatalf("receive upstream proxy chunk: %v", err)
	}
	if len(encoded) < 2 || encoded[0] != 0x0a {
		t.Fatalf("invalid upstream proxy chunk: %v", encoded)
	}
	length := 0
	shift := 0
	index := 1
	for ; index < len(encoded); index++ {
		value := encoded[index]
		length |= int(value&0x7f) << shift
		if value&0x80 == 0 {
			index++
			break
		}
		shift += 7
	}
	if index+length != len(encoded) {
		t.Fatalf("invalid upstream proxy chunk length: %v", encoded)
	}
	return encoded[index:]
}
