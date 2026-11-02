package telemetry

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	tracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestSetupDisabled(t *testing.T) {
	before := otel.GetTracerProvider()
	runtime, err := Setup(t.Context(), config.TelemetryConfig{}, "test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.LogHandler() != nil {
		t.Fatal("disabled telemetry has a log handler")
	}
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if otel.GetTracerProvider() != before {
		t.Fatal("disabled setup changed tracer provider")
	}
}

func TestOTLPEndpointsAndHeaders(t *testing.T) {
	if headers := otlpHeaders(config.TelemetryOTLPConfig{}, ""); len(headers) != 0 {
		t.Fatalf("unexpected headers: %v", headers)
	}
	for _, base := range []string{"https://observe.test", "https://observe.test/"} {
		cfg := config.TelemetryOTLPConfig{BaseURL: base, Organization: "team/name", Username: "writer", Password: "password"}
		if got := otlpURL(cfg, "logs"); got != "https://observe.test/api/team%2Fname/v1/logs" {
			t.Fatalf("unexpected endpoint: %s", got)
		}
		headers := otlpHeaders(cfg, "oma_server")
		if headers["stream-name"] != "oma_server" || headers["Authorization"] != "Basic "+base64.StdEncoding.EncodeToString([]byte("writer:password")) {
			t.Fatal("missing OTLP headers")
		}
	}
}

func TestSetupExportsAllSignals(t *testing.T) {
	oldTrace, oldMeter, oldLogger := otel.GetTracerProvider(), otel.GetMeterProvider(), global.GetLoggerProvider()
	oldPropagator, oldErrorHandler := otel.GetTextMapPropagator(), otel.GetErrorHandler()
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTrace)
		otel.SetMeterProvider(oldMeter)
		global.SetLoggerProvider(oldLogger)
		otel.SetTextMapPropagator(oldPropagator)
		otel.SetErrorHandler(oldErrorHandler)
	})
	var mu sync.Mutex
	received := map[string][]byte{}
	headers := map[string]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusBadRequest)
			return
		}
		mu.Lock()
		received[r.URL.Path] = body
		headers[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := config.TelemetryConfig{
		Enabled: true, ServiceName: "oma-server",
		OTLP:    config.TelemetryOTLPConfig{BaseURL: server.URL, Organization: "oma", Username: "writer", Password: "password", TracesStream: "oma_server", LogsStream: "oma_server", Timeout: time.Second},
		Traces:  config.TelemetryTracesConfig{Enabled: true},
		Metrics: config.TelemetryMetricsConfig{Enabled: true, ExportInterval: time.Hour},
		Logs:    config.TelemetryLogsConfig{Enabled: true, Level: "info"},
	}
	runtime, err := Setup(t.Context(), cfg, "test", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, Remote: true})
	ctx, span := otel.Tracer(InstrumentationName).Start(trace.ContextWithRemoteSpanContext(t.Context(), parent), "test.export")
	if !span.IsRecording() || !span.SpanContext().IsSampled() {
		t.Fatal("AlwaysSample must record an unsampled parent's child")
	}
	slog.New(runtime.LogHandler()).InfoContext(ctx, "test log")
	counter, err := otel.Meter(InstrumentationName).Int64Counter("test.counter")
	if err != nil {
		t.Fatal(err)
	}
	counter.Add(ctx, 1)
	span.End()
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, signal := range []string{"traces", "metrics", "logs"} {
		path := "/api/oma/v1/" + signal
		if len(received[path]) == 0 {
			t.Fatalf("missing %s export", signal)
		}
		if headers[path].Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("writer:password")) {
			t.Fatalf("missing %s authentication", signal)
		}
		wantStream := "oma_server"
		if signal == "metrics" {
			wantStream = ""
		}
		if headers[path].Get("stream-name") != wantStream {
			t.Fatalf("wrong %s stream", signal)
		}
	}
	var exported tracepb.ExportTraceServiceRequest
	if err := proto.Unmarshal(received["/api/oma/v1/traces"], &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.ResourceSpans) != 1 {
		t.Fatalf("resource spans = %d", len(exported.ResourceSpans))
	}
	attrs := map[string]string{}
	for _, attr := range exported.ResourceSpans[0].Resource.Attributes {
		attrs[attr.Key] = attr.Value.GetStringValue()
	}
	if attrs["service.name"] != "oma-server" || attrs["deployment.environment.name"] != "test" || attrs["service.instance.id"] == "" || attrs["service.version"] == "" {
		t.Fatalf("resource = %v", attrs)
	}
}

func TestRuntimeMetrics(t *testing.T) {
	previousLimit := debug.SetMemoryLimit(1 << 40)
	t.Cleanup(func() { debug.SetMemoryLimit(previousLimit) })
	reader := sdkmetric.NewManualReader(sdkmetric.WithProducer(otelruntime.NewProducer()))
	provider, err := newRuntimeMeterProvider(reader, resource.Empty())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var data metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &data); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			names[metric.Name] = true
		}
	}
	for _, name := range []string{
		"go.goroutine.count", "go.memory.used", "go.memory.limit", "go.memory.allocated", "go.memory.allocations",
		"go.memory.gc.goal", "go.processor.limit", "go.config.gogc", "go.schedule.duration",
	} {
		if !names[name] {
			t.Errorf("runtime metric %s not produced: %v", name, names)
		}
	}
}
