package e2bruntime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestProviderTelemetryNotFound(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"private provider detail"}`, http.StatusNotFound)
	}))
	defer server.Close()
	provider := NewProvider(config.E2BConfig{APIKey: "e2b_0000000000000000000000000000000000000000", APIURL: server.URL, SandboxURL: server.URL, RequestTimeout: time.Second})
	if err := provider.SetTimeout(t.Context(), "sbx_private", time.Minute); !errors.Is(err, sandboxruntime.ErrSandboxNotFound) {
		t.Fatalf("not found behavior: %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "oma.e2b.set_timeout" || spans[0].Status().Code != codes.Error || len(spans[0].Events()) != 0 {
		t.Fatalf("provider spans: %v", spans)
	}
	attrs := map[string]string{}
	for _, attr := range spans[0].Attributes() {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}
	if attrs["oma.operation"] != "set_timeout" || attrs["oma.provider_sandbox.id"] != "sbx_private" || attrs["error.type"] != "error" || len(attrs) != 3 {
		t.Fatalf("provider attributes: %v", attrs)
	}
	telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "oma.sandbox.provider.duration", 0, map[string]string{"oma.operation": "set_timeout", "oma.outcome": "not_found"})
}
