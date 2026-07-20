package codesessions

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"

	"golang.org/x/net/websocket"
)

// connectViaTunnel 驱动一次完整的 CONNECT 握手，返回 framed 状态行与 dial 是否被调用。
func connectViaTunnel(t *testing.T, handler *Handler, target string) (string, bool) {
	t.Helper()
	dialed := make(chan string, 1)
	handler.upstreamProxy = upstreamProxyRuntime{
		dial: func(_ context.Context, dialTarget string) (net.Conn, error) {
			dialed <- dialTarget
			proxySide, targetSide := net.Pipe()
			t.Cleanup(func() { targetSide.Close() })
			return proxySide, nil
		},
	}
	server := httptest.NewServer(websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(connection *websocket.Conn) {
			handler.serveUpstreamProxyTunnel(connection, testUpstreamProxyIdentity(), "sk-ant-si-test")
		},
	})
	t.Cleanup(server.Close)

	wsConfig, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http"), "http://localhost")
	if err != nil {
		t.Fatalf("NewConfig() error = %v", err)
	}
	connection, err := websocket.DialConfig(wsConfig)
	if err != nil {
		t.Fatalf("DialConfig() error = %v", err)
	}
	t.Cleanup(func() { connection.Close() })

	authorization := base64.StdEncoding.EncodeToString([]byte("cse_test:sk-ant-si-test"))
	connectHead := "CONNECT " + target + " HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
	if err := websocket.Message.Send(connection, encodeUpstreamProxyChunk([]byte(connectHead))); err != nil {
		t.Fatalf("send CONNECT chunk: %v", err)
	}
	status, err := receiveUpstreamProxyChunk(connection)
	if err != nil {
		t.Fatalf("receive CONNECT status: %v", err)
	}
	select {
	case <-dialed:
		return string(status), true
	case <-time.After(200 * time.Millisecond):
		return string(status), false
	}
}

func testUpstreamProxyIdentity() upstreamProxyIdentity {
	return upstreamProxyIdentity{
		codeSessionExternalID: "cse_test",
		organizationUUID:      "00000000-0000-0000-0000-000000000001",
		workspaceUUID:         "00000000-0000-0000-0000-000000000002",
	}
}

// stubUnrestrictedPolicyContext 给 nil DB 的测试 Handler 提供 unrestricted 策略上下文。
func stubUnrestrictedPolicyContext(t *testing.T, handler *Handler) {
	t.Helper()
	policy, err := networkpolicy.ParsePolicy(json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`), nil)
	if err != nil {
		t.Fatalf("ParsePolicy() error = %v", err)
	}
	handler.loadPolicyContext = func(context.Context, upstreamProxyIdentity) (upstreamProxyPolicyContext, error) {
		return upstreamProxyPolicyContext{policy: policy}, nil
	}
}

func policyTestHandler(t *testing.T, policy networkpolicy.Policy, err error) *Handler {
	t.Helper()
	handler := NewHandler(config.Config{}, newTestService(t, nil))
	handler.loadPolicyContext = func(context.Context, upstreamProxyIdentity) (upstreamProxyPolicyContext, error) {
		return upstreamProxyPolicyContext{
			policy:                policy,
			organizationID:        1,
			workspaceID:           2,
			environmentExternalID: "env_test",
		}, err
	}
	return handler
}

func mustParsePolicy(t *testing.T, configRaw json.RawMessage) networkpolicy.Policy {
	t.Helper()
	policy, err := networkpolicy.ParsePolicy(configRaw, nil)
	if err != nil {
		t.Fatalf("ParsePolicy() error = %v", err)
	}
	return policy
}

// ---- 失败场景 ----

func TestUpstreamProxyPolicyDeniesLimitedUnlistedHost(t *testing.T) {
	t.Parallel()

	handler := policyTestHandler(t, mustParsePolicy(t,
		json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}`)), nil)
	status, dialed := connectViaTunnel(t, handler, "1.1.1.1:443")
	if !strings.HasPrefix(status, "HTTP/1.1 403") {
		t.Fatalf("limited CONNECT status = %q, want 403", status)
	}
	if dialed {
		t.Fatal("policy-denied target must not be dialed")
	}
}

func TestUpstreamProxyPolicyFailsClosedWhenSubjectUnavailable(t *testing.T) {
	t.Parallel()

	handler := policyTestHandler(t, networkpolicy.Policy{}, errors.New("database unavailable"))
	status, dialed := connectViaTunnel(t, handler, "1.1.1.1:443")
	if !strings.HasPrefix(status, "HTTP/1.1 403") {
		t.Fatalf("unavailable-policy CONNECT status = %q, want 403", status)
	}
	if dialed {
		t.Fatal("policy-unavailable target must not be dialed")
	}
}

func TestUpstreamProxyPolicyFailureLogOmitsUnavailableInternalIDs(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := policyTestHandler(t, networkpolicy.Policy{}, errors.New("database unavailable"))
	if handler.authorizeUpstreamProxyTarget(context.Background(), testUpstreamProxyIdentity(), "example.com:443") {
		t.Fatal("unavailable policy must be denied")
	}
	logged := output.String()
	for _, field := range []string{"organization_id", "workspace_id", "environment_id"} {
		if strings.Contains(logged, field+"=") {
			t.Fatalf("failure log contains unavailable internal field %q: %s", field, logged)
		}
	}
}

func TestUpstreamProxyPolicyDeniesBeforeSSRFResolution(t *testing.T) {
	t.Parallel()

	// limited 拒绝私网目标时必须先命中策略 403，而不是进入 SSRF/DNS 路径；
	// 这里用公网目标避免与 SSRF 拒绝混淆，dial 不发生即证明策略先行。
	handler := policyTestHandler(t, mustParsePolicy(t,
		json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":false,"allow_package_managers":false}}`)), nil)
	status, dialed := connectViaTunnel(t, handler, "8.8.8.8:443")
	if !strings.HasPrefix(status, "HTTP/1.1 403") {
		t.Fatalf("unlisted public target status = %q, want 403", status)
	}
	if dialed {
		t.Fatal("unlisted target must not be dialed")
	}
}

// ---- 成功场景 ----

func TestUpstreamProxyPolicyAllowsListedHost(t *testing.T) {
	t.Parallel()

	handler := policyTestHandler(t, mustParsePolicy(t,
		json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["1.1.1.1"],"allow_mcp_servers":false,"allow_package_managers":false}}`)), nil)
	status, dialed := connectViaTunnel(t, handler, "1.1.1.1:443")
	if !strings.HasPrefix(status, "HTTP/1.1 200 Connection Established") {
		t.Fatalf("listed CONNECT status = %q, want 200", status)
	}
	if !dialed {
		t.Fatal("listed target must be dialed")
	}
}

func TestUpstreamProxyPolicyAllowsUnrestricted(t *testing.T) {
	t.Parallel()

	handler := policyTestHandler(t, mustParsePolicy(t,
		json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`)), nil)
	status, dialed := connectViaTunnel(t, handler, "1.1.1.1:443")
	if !strings.HasPrefix(status, "HTTP/1.1 200 Connection Established") {
		t.Fatalf("unrestricted CONNECT status = %q, want 200", status)
	}
	if !dialed {
		t.Fatal("unrestricted target must be dialed")
	}
}
