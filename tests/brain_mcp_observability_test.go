//go:build integration

package tests

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func TestBrainMCPGatewayTelemetry(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	for _, tc := range []struct {
		name, body, outcome, method, tool string
		upstreamStatus                    int
		contentType, reply                string
	}{
		{"invalid request", `{"jsonrpc":"1.0"}`, "invalid_request", "other", "", 0, "", ""},
		{"upstream status", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read"}}`, "upstream_status", "tools/call", "read", http.StatusServiceUnavailable, "application/json", ""},
		{"rpc error", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"secret-tool"}}`, "rpc_error", "tools/call", "other", http.StatusOK, "application/json", `{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"unknown tool"}}`},
		{"tool error", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bash"}}`, "tool_error", "tools/call", "bash", http.StatusOK, "application/json", `{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[]}}`},
		{"streamed tool error", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bash"}}`, "tool_error", "tools/call", "bash", http.StatusOK, "text/event-stream", "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"isError\":true}}\n\n"},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"private/secret-method"}`, "ok", "other", "", http.StatusOK, "application/json", ""},
		{"success", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read"}}`, "ok", "tools/call", "read", http.StatusOK, "application/json", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstreamTrace := make(chan string, 1)
			app, worker, _, token := newBrainMCPProxyApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamTrace <- r.Header.Get(telemetry.TraceParentHeader)
				reply := tc.reply
				if reply == "" {
					reply = `{"jsonrpc":"2.0","id":1,"result":{}}`
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.upstreamStatus)
				_, _ = io.WriteString(w, reply)
			}))
			before := len(recorder.Ended())
			response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp", strings.NewReader(tc.body), token, false)
			response.Body.Close()
			var gateway sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended()[before:] {
				if span.Name() == "oma.sandbox_mcp.call" {
					gateway = span
				}
			}
			if gateway == nil {
				t.Fatal("gateway span missing")
			}
			attrs := map[string]string{}
			for _, attr := range gateway.Attributes() {
				attrs[string(attr.Key)] = attr.Value.Emit()
			}
			if attrs["oma.outcome"] != tc.outcome || attrs["oma.mcp.method"] != tc.method || attrs["oma.tool.name"] != tc.tool || attrs["oma.code_session.id"] != worker.ExternalID {
				t.Fatalf("gateway attributes: %v", attrs)
			}
			if failed := gateway.Status().Code == codes.Error; failed != (tc.outcome != "ok") {
				t.Fatalf("gateway status %v for outcome %s", gateway.Status(), tc.outcome)
			}
			for _, value := range attrs {
				if strings.Contains(value, "secret-method") || strings.Contains(value, "secret-tool") || strings.Contains(value, token) {
					t.Fatalf("sensitive value in span: %v", attrs)
				}
			}
			if tc.upstreamStatus == 0 {
				return
			}
			link, ok := telemetry.LinkFromTraceParent(<-upstreamTrace)
			if !ok || link.SpanContext.SpanID() != gateway.SpanContext().SpanID() {
				t.Fatal("upstream traceparent does not reference the gateway span")
			}
		})
	}
}
