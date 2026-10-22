package tunnels

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestServeTunnelMarksInvalidTargetAsNotForwarded(t *testing.T) {
	handler := &IngressHandler{cfg: config.TunnelConfig{PublicBaseURL: "https://oma.example"}, errorAdapter: httpapi.NewErrorAdapter(slog.New(slog.DiscardHandler))}
	target, err := url.Parse("https://oma.example/v1/mcp/not-a-tunnel")
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, "/proxy", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))

		if !handler.ServeTunnel(recorder, request, "organization", "workspace", target) {
			t.Fatal("tunnel target was not claimed")
		}

		if recorder.Header().Get(NotForwardedHeader) != "true" || recorder.Code < http.StatusBadRequest {
			t.Fatalf("%s status=%d not-forwarded=%q", method, recorder.Code, recorder.Header().Get(NotForwardedHeader))
		}
	}
}

func TestDispatchTunnelReportsFailuresBeforeQueueing(t *testing.T) {
	archived := time.Now()
	handler := &IngressHandler{cfg: brokerTestConfig()}
	for name, tunnel := range map[string]db.MCPTunnel{
		"archived tunnel": {UUID: "tunnel", ExternalID: "tunnel", ArchivedAt: &archived},
		"invalid channel": {UUID: "tunnel", ExternalID: "tunnel"},
	} {
		channel := "main"
		if name == "invalid channel" {
			channel = "Not A Channel"
		}
		var queued bool
		request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))

		err := handler.dispatchTunnel(httptest.NewRecorder(), request, tunnel, channel, CommandTypeJSONRPC, &queued)

		if err == nil || queued {
			t.Fatalf("%s: err=%v queued=%v", name, err, queued)
		}
	}
}

func TestDispatchTunnelReportsQueuedCommandsAfterLaterFailure(t *testing.T) {
	cfg := brokerTestConfig()
	cfg.RequestTimeout = 200 * time.Millisecond
	broker := testNATSBroker(t, cfg)
	handler := &IngressHandler{cfg: cfg, broker: broker}
	var queued bool
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`))

	err := handler.dispatchTunnel(httptest.NewRecorder(), request, db.MCPTunnel{UUID: "tunnel", ExternalID: "tunnel"}, "main", CommandTypeJSONRPC, &queued)

	if err == nil || !queued {
		t.Fatalf("err=%v queued=%v", err, queued)
	}
}
