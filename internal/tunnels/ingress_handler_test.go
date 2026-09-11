package tunnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestShouldCancelTunnelRequestForCallerAndDeadlineTermination(t *testing.T) {
	t.Parallel()
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("wrapped: %w", context.DeadlineExceeded)} {
		if !shouldCancelTunnelRequest(err) {
			t.Fatalf("shouldCancelTunnelRequest(%v) = false, want true", err)
		}
	}
	if shouldCancelTunnelRequest(errors.New("broker unavailable")) {
		t.Fatal("shouldCancelTunnelRequest(broker unavailable) = true, want false")
	}
}

func TestWriteSSEMessageCompactsMultilineJSON(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writeSSEMessage(recorder, json.RawMessage("{\n  \"jsonrpc\": \"2.0\",\n  \"method\": \"notifications/progress\"\n}"))
	want := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"
	if recorder.Body.String() != want {
		t.Fatalf("SSE body = %q, want %q", recorder.Body.String(), want)
	}
}

func TestWriteIngressResponseFramesTerminalEventStream(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writeIngressResponse(recorder, TunnelResponse{
		ResponseCode:    http.StatusOK,
		ResponseType:    ResponseTypeJSONRPC,
		ResponseHeaders: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}},
		JSONResponse:    json.RawMessage(`{"jsonrpc":"2.0","id":0,"result":{}}`),
	}, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "no-cache" {
		t.Fatalf("Cache-Control = %q, want no-cache", cacheControl)
	}
	want := "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":0,\"result\":{}}\n\n"
	if recorder.Body.String() != want {
		t.Fatalf("body = %q, want %q", recorder.Body.String(), want)
	}
}

func TestWriteIngressResponseKeepsTerminalJSON(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	writeIngressResponse(recorder, TunnelResponse{
		ResponseCode:    http.StatusOK,
		ResponseType:    ResponseTypeJSONRPC,
		ResponseHeaders: http.Header{"Content-Type": {"application/json"}},
		JSONResponse:    json.RawMessage(`{"jsonrpc":"2.0","id":0,"result":{}}`),
	}, "")

	if body := recorder.Body.String(); body != `{"jsonrpc":"2.0","id":0,"result":{}}` {
		t.Fatalf("body = %q", body)
	}
}

func TestIngressGETReturnsExplicitMethodNotAllowed(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/mcp/tunnel_0123456789abcdef0123456789abcdef", nil)

	(&IngressHandler{}).getSSENotSupported(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
	if allow := recorder.Header().Get("Allow"); allow != "POST, DELETE" {
		t.Fatalf("Allow = %q, want POST, DELETE", allow)
	}
	var envelope struct {
		Type  string `json:"type"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Type != "error" || envelope.Error.Type != "invalid_request_error" {
		t.Fatalf("error envelope = %+v", envelope)
	}
}

func TestServeTunnelLeavesOrdinaryMCPURLsOnProxyPath(t *testing.T) {
	t.Parallel()
	target, err := url.Parse("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	handler := &IngressHandler{}
	if handler.ServeTunnel(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/proxy", nil),
		"organization",
		"workspace",
		target,
	) {
		t.Fatal("ordinary MCP URL was claimed by TunnelInvoker")
	}
}

func TestSplitTunnelPath(t *testing.T) {
	t.Parallel()
	tests := map[string][]string{
		"":       nil,
		"/":      nil,
		"/main/": {"main"},
		"/v1/mcp/tunnel_0123456789abcdef0123456789abcdef":    {"v1", "mcp", "tunnel_0123456789abcdef0123456789abcdef"},
		"/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/ch": {"v1", "mcp", "tunnel_0123456789abcdef0123456789abcdef", "ch"},
	}
	for path, want := range tests {
		if got := splitTunnelPath(path); !reflect.DeepEqual(got, want) {
			t.Fatalf("splitTunnelPath(%q) = %#v, want %#v", path, got, want)
		}
	}
}
