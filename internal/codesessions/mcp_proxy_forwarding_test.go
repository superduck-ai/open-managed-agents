package codesessions

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestMCPProxyMarksPolicyRejectionAsNotForwarded(t *testing.T) {
	response := postMCPProxyToolCall(t, "https://allowed.example.test/mcp", "https://denied.example.test/mcp")

	if response.StatusCode != http.StatusForbidden || response.Header.Get(mcpNotForwardedHeader) != "true" {
		t.Fatalf("status=%d not-forwarded=%q", response.StatusCode, response.Header.Get(mcpNotForwardedHeader))
	}
}

func TestMCPProxyMarksRefusedUpstreamAsNotForwarded(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	target := "http://" + listener.Addr().String() + "/mcp"
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	response := postMCPProxyToolCall(t, target, target)

	if response.StatusCode != http.StatusBadGateway || response.Header.Get(mcpNotForwardedHeader) != "true" {
		t.Fatalf("status=%d not-forwarded=%q", response.StatusCode, response.Header.Get(mcpNotForwardedHeader))
	}
}

func TestMCPProxyKeepsLostUpstreamResponseUnknown(t *testing.T) {
	received := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- body
		connection, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(upstream.Close)

	response := postMCPProxyToolCall(t, upstream.URL+"/mcp", upstream.URL+"/mcp")

	if response.StatusCode != http.StatusBadGateway || response.Header.Get(mcpNotForwardedHeader) != "" {
		t.Fatalf("status=%d not-forwarded=%q", response.StatusCode, response.Header.Get(mcpNotForwardedHeader))
	}
	if got := <-received; !bytes.Equal(got, mcpProxyToolCallBody) {
		t.Fatalf("upstream received %q", got)
	}
}

func TestMCPProxyForwardedResponseIsNotMarked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	t.Cleanup(upstream.Close)

	response := postMCPProxyToolCall(t, upstream.URL+"/mcp", upstream.URL+"/mcp")

	if response.StatusCode != http.StatusOK || response.Header.Get(mcpNotForwardedHeader) != "" {
		t.Fatalf("status=%d not-forwarded=%q", response.StatusCode, response.Header.Get(mcpNotForwardedHeader))
	}
}

func TestNamedMCPTunnelMarksOnlyUnservedRequestsAsNotForwarded(t *testing.T) {
	targetURL := "https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef"
	for _, served := range []bool{false, true} {
		handler, token := newNamedMCPTestHandler(t, "local_tunnel", targetURL)
		handler.tunnelInvoker = tunnelInvokerFuncs{serve: func(w http.ResponseWriter, _ *http.Request, _, _ string, _ *url.URL) bool {
			if !served {
				return false
			}
			w.WriteHeader(http.StatusBadGateway)
			return true
		}}
		router := chi.NewRouter()
		router.Route("/v2", handler.RegisterV2Routes)
		request := httptest.NewRequest(http.MethodPost, "/v2/ccr-sessions/cse_test/mcp/local_tunnel", bytes.NewReader(mcpProxyToolCallBody))
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)

		marked := recorder.Header().Get(mcpNotForwardedHeader) == "true"
		if marked == served {
			t.Fatalf("served=%v status=%d not-forwarded=%v", served, recorder.Code, marked)
		}
	}
}

var mcpProxyToolCallBody = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{}}}`)

func postMCPProxyToolCall(t *testing.T, allowedMCPURL, target string) *http.Response {
	t.Helper()
	handler, token := newMCPProxyTestHandler(t, allowedMCPURL, slog.New(slog.DiscardHandler))
	handler.mcpProxyTransport = http.DefaultTransport
	router := chi.NewRouter()
	router.Route("/v2", handler.RegisterV2Routes)
	proxyURL := "/v2/ccr-sessions/cse_test/mcp?" + url.Values{"mcp_url": {target}}.Encode()
	request := httptest.NewRequest(http.MethodPost, proxyURL, bytes.NewReader(mcpProxyToolCallBody))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder.Result()
}
