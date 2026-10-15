package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

func TestHTTPTraceMiddleware(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	previousMeter := otel.GetMeterProvider()
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetTracerProvider(provider)
	otel.SetMeterProvider(meterProvider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		otel.SetMeterProvider(previousMeter)
		_ = provider.Shutdown(context.Background())
		_ = meterProvider.Shutdown(context.Background())
	})
	t.Run("excluded paths", func(t *testing.T) {
		paths := []string{"/healthz", "/readyz", "/v1/sessions/sesn_a/stream", "/v1/code/sessions/cse_a/stream", "/web-api/sessions/sesn_a/stream", "/v1/code/upstreamproxy/ws", "/v1/code/sessions/cse_a/worker/heartbeat", "/v1/code/sessions/cse_a/worker/otlp/logs", "/v1/filestore/fs_a/file", "/connector/poll"}
		before := len(recorder.Ended())
		handler := telemetryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
		for _, path := range paths {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		}
		if len(recorder.Ended()) != before {
			t.Fatal("excluded paths produced spans")
		}
		var measurements metricdata.ResourceMetrics
		if err := reader.Collect(t.Context(), &measurements); err != nil {
			t.Fatal(err)
		}
		if len(measurements.ScopeMetrics) != 0 {
			t.Fatal("excluded paths produced metrics")
		}
	})
	t.Run("unmatched route", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(telemetryMiddleware)
		router.Get("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/unknown-secret-id", nil))
		spans := recorder.Ended()
		if got := spans[len(spans)-1].Name(); got != "GET unmatched" {
			t.Fatalf("span name = %q", got)
		}
	})
	t.Run("server error", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(telemetryMiddleware)
		router.Get("/failure", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/failure", nil))
		spans := recorder.Ended()
		if spans[len(spans)-1].Status().Code != codes.Error {
			t.Fatal("server error span is not an error")
		}
	})
	t.Run("panic recovered before span ends", func(t *testing.T) {
		server := &Server{logger: slog.New(slog.DiscardHandler)}
		router := chi.NewRouter()
		router.Use(telemetryMiddleware)
		router.Use(server.recoverMiddleware)
		router.Get("/panic", func(http.ResponseWriter, *http.Request) { panic("test panic") })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
		spans := recorder.Ended()
		if response.Code != 500 || spans[len(spans)-1].Status().Code != codes.Error {
			t.Fatal("panic was not observed as 500")
		}
	})
	t.Run("route principal and log correlation", func(t *testing.T) {
		var logs bytes.Buffer
		logger := slog.New(logging.NewContextHandler(slog.NewJSONHandler(&logs, nil), telemetry.LogContextAttrs))
		router := chi.NewRouter()
		router.Use(telemetryMiddleware)
		router.Use(requestLoggingMiddleware(logger))
		router.Post("/v1/sessions/{session_id}/events", func(w http.ResponseWriter, r *http.Request) {
			annotatePrincipalSpan(r.Context(), auth.Principal{OrganizationUUID: "org-test", WorkspaceUUID: "workspace-test"})
			w.WriteHeader(http.StatusCreated)
		})
		request := httptest.NewRequest(http.MethodPost, "/v1/sessions/sesn_test/events?token=secret-query", nil)
		request.Header.Set("traceparent", "00-01010101010101010101010101010101-0202020202020202-01")
		parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
		request = request.WithContext(httpapi.WithRequestID(trace.ContextWithSpanContext(request.Context(), parent), "req_test"))
		router.ServeHTTP(httptest.NewRecorder(), request)
		spans := recorder.Ended()
		span := spans[len(spans)-1]
		if span.Name() != "POST /v1/sessions/{session_id}/events" || span.Parent().IsValid() || len(span.Links()) != 0 {
			t.Fatalf("unexpected span name or parent: %s %v", span.Name(), span.Parent())
		}
		attrs := map[string]string{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.Emit()
		}
		for key, value := range map[string]string{"oma.session.id": "sesn_test", "oma.request.id": "req_test", "oma.organization.uuid": "org-test", "oma.workspace.uuid": "workspace-test", "http.route": "/v1/sessions/{session_id}/events"} {
			if attrs[key] != value {
				t.Fatalf("%s = %q, want %q", key, attrs[key], value)
			}
		}
		if len(span.Events()) != 0 || strings.Contains(logs.String(), "secret-query") {
			t.Fatal("unexpected span event or sensitive query")
		}
		for _, record := range parseSlogJSONLines(t, logs.String()) {
			if record["trace_id"] != span.SpanContext().TraceID().String() || record["span_id"] != span.SpanContext().SpanID().String() {
				t.Fatalf("log lacks trace correlation: %v", record)
			}
		}
	})
}
