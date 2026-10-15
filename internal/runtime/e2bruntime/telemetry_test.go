package e2bruntime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

func TestProviderTelemetryNotFound(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previousTrace, previousMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	otel.SetTracerProvider(traces)
	otel.SetMeterProvider(metrics)
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTrace)
		otel.SetMeterProvider(previousMeter)
		_ = traces.Shutdown(context.Background())
		_ = metrics.Shutdown(context.Background())
	})
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
	var data metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &data); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "oma.sandbox.provider.duration" {
				continue
			}
			for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				operation, _ := point.Attributes.Value(attribute.Key("oma.operation"))
				outcome, _ := point.Attributes.Value(attribute.Key("oma.outcome"))
				if operation.AsString() == "set_timeout" && outcome.AsString() == "not_found" {
					found = true
					if point.Count != 1 || point.Attributes.Len() != 2 {
						t.Fatalf("provider metric: %+v", point)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("not_found provider metric missing")
	}
}
