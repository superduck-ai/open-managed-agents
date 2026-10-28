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
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestHTTPTraceMiddleware(t *testing.T) {
	recorder := telemetrytest.SpanRecorder(t)
	reader := telemetrytest.MetricReader(t)
	t.Run("excluded routes produce no spans or metrics", func(t *testing.T) {
		before := len(recorder.Ended())
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
		noContent := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
		router.Get("/healthz", noContent)
		router.Get("/v1/code/sessions/{code_session_id}/worker/events/stream", noContent)
		router.Route("/v1/code/brain/claims", func(r chi.Router) { r.Post("/", noContent) })
		router.Mount("/connector", http.HandlerFunc(noContent))
		for _, request := range []*http.Request{
			httptest.NewRequest(http.MethodGet, "/healthz", nil),
			httptest.NewRequest(http.MethodGet, "/v1/code/sessions/cse_a/worker/events/stream", nil),
			httptest.NewRequest(http.MethodPost, "/v1/code/brain/claims", nil),
			httptest.NewRequest(http.MethodGet, "/connector/poll", nil),
		} {
			router.ServeHTTP(httptest.NewRecorder(), request)
		}
		if len(recorder.Ended()) != before {
			t.Fatal("excluded routes produced spans")
		}
		if data := telemetrytest.Collect(t, reader); len(data) != 0 {
			t.Fatal("excluded routes produced metrics")
		}
	})
	t.Run("heartbeat measured without span", func(t *testing.T) {
		before := len(recorder.Ended())
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
		router.Post("/v1/code/sessions/{code_session_id}/worker/heartbeat", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusGone) })
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/code/sessions/cse_a/worker/heartbeat", nil))
		if len(recorder.Ended()) != before {
			t.Fatal("heartbeat produced a span")
		}
		telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "http.server.request.duration", 0, map[string]string{
			"http.request.method": "POST", "http.route": "/v1/code/sessions/{code_session_id}/worker/heartbeat", "http.response.status_code": "410",
		})
	})
	t.Run("worker routes link caller trace without parenting", func(t *testing.T) {
		traceParent := "00-01010101010101010101010101010101-0202020202020202-01"
		handler := telemetryMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
		for path, links := range map[string]int{
			"/v1/agents": 0,
			"/v1/code/sessions/cse_a/worker/register": 1,
			"/v1/code/sessions/cse_a/worker":          0,
			"/v2/ccr-sessions/cse_a/sandbox/mcp":      1,
			"/v2/sessions/cse_a":                      1,
			"/v1/messages":                            1,
		} {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			if path != "/v1/code/sessions/cse_a/worker" {
				request.Header.Set("traceparent", traceParent)
			} else {
				request.Header.Set("traceparent", "invalid")
			}
			handler.ServeHTTP(httptest.NewRecorder(), request)
			spans := recorder.Ended()
			span := spans[len(spans)-1]
			if span.Parent().IsValid() || len(span.Links()) != links {
				t.Fatalf("%s: parent=%v links=%d", path, span.Parent(), len(span.Links()))
			}
			if links == 1 && span.Links()[0].SpanContext.TraceID().String() != "01010101010101010101010101010101" {
				t.Fatalf("%s: wrong link", path)
			}
		}
	})
	t.Run("unmatched route", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
		router.Get("/known", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/unknown-secret-id", nil))
		spans := recorder.Ended()
		if got := spans[len(spans)-1].Name(); got != "GET unmatched" {
			t.Fatalf("span name = %q", got)
		}
	})
	t.Run("server error", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
		router.Get("/failure", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/failure", nil))
		spans := recorder.Ended()
		if spans[len(spans)-1].Status().Code != codes.Error {
			t.Fatal("server error span is not an error")
		}
	})
	t.Run("client canceled server error", func(t *testing.T) {
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
		router.Get("/canceled", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/canceled", nil).WithContext(ctx))
		spans := recorder.Ended()
		span := spans[len(spans)-1]
		attrs := map[string]string{}
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.Emit()
		}
		if span.Status().Code == codes.Error || attrs["http.response.status_code"] != "499" || attrs["oma.client_canceled"] != "true" {
			t.Fatalf("canceled request span: status=%v attrs=%v", span.Status(), attrs)
		}
	})
	t.Run("panic recovered before span ends", func(t *testing.T) {
		server := &Server{logger: slog.New(slog.DiscardHandler)}
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
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
		logger := slog.New(logging.NewContextHandler(slog.NewJSONHandler(&logs, nil), traceLogAttrs))
		router := chi.NewRouter()
		router.Use(telemetryMiddleware(router))
		router.Use(requestLoggingMiddleware(logger, router))
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

func traceLogAttrs(ctx context.Context) []slog.Attr {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return nil
	}
	return []slog.Attr{slog.String("trace_id", spanContext.TraceID().String()), slog.String("span_id", spanContext.SpanID().String())}
}

func TestRequestTelemetryPolicyFollowsServerRoutes(t *testing.T) {
	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerDeps{CodeSessionCredentials: credentials, FilestoreCredentials: filestoreCredentials})
	for _, tc := range []struct {
		method string
		path   string
		want   requestTelemetry
	}{
		{http.MethodGet, "/v1/unknown-route", tracedRequest},
		{http.MethodGet, "/v1/sessions/sesn_a/events", tracedRequest},
		{http.MethodPost, "/v1/messages", tracedRequest},
		{http.MethodPost, "/v1/code/brain/claims/cse_a/release", tracedRequest},
		{http.MethodGet, "/v1/code/upstreamproxy/ca-cert", tracedRequest},
		{http.MethodPost, "/v1/code/sessions/cse_a/worker/events", tracedRequest},
		{http.MethodGet, "/healthz", unobservedRequest},
		{http.MethodGet, "/readyz", unobservedRequest},
		{http.MethodGet, "/v1/sessions/sesn_a/events/stream", unobservedRequest},
		{http.MethodGet, "/v1/sessions/sesn_a/threads/sthr_a/stream", unobservedRequest},
		{http.MethodGet, "/web-api/sessions/sesn_a/stream", unobservedRequest},
		{http.MethodGet, "/v1/code/upstreamproxy/ws", unobservedRequest},
		{http.MethodGet, "/v1/code/sessions/cse_a/worker/events/stream", unobservedRequest},
		{http.MethodPost, "/v1/code/sessions/cse_a/worker/otlp/v1/logs", unobservedRequest},
		{http.MethodPost, "/v1/code/sessions/cse_a/worker/otlp/v1/traces", unobservedRequest},
		{http.MethodPut, "/v1/filestore/fs/fs_a/file", unobservedRequest},
		{http.MethodPost, "/v1/code/sessions/cse_a/worker/heartbeat", measuredRequest},
		{http.MethodPost, "/v1/code/brain/claims", longPollRequest},
	} {
		if got := requestTelemetryFor(server.router, httptest.NewRequest(tc.method, tc.path, nil)); got != tc.want {
			t.Errorf("%s %s: policy=%+v want %+v", tc.method, tc.path, got, tc.want)
		}
	}
	resolved := map[string]bool{}
	if err := chi.Walk(server.router, func(_, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		resolved[route] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for pattern := range routeTelemetry {
		if !resolved[pattern] && !resolved[pattern+"/"] && pattern != "/connector/*" {
			t.Errorf("telemetry policy pattern %q is not registered", pattern)
		}
	}
}
