package messages

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

func TestModelObservationErrorTypes(t *testing.T) {
	for _, input := range []string{"invalid_response", "incomplete_response", "provider_error", "transport_error", "http_error", "stream_error", "cancelled", "observation_limit", ""} {
		want := input
		if input == "invalid_response" || input == "incomplete_response" || input == "provider_error" {
			want = "stream_error"
		}
		if got := modelObservationErrorType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}

func TestResponseObservationFirstToken(t *testing.T) {
	observation := &responseObservation{blocks: map[int]*responseContentBlock{0: {Type: "text"}}}
	started := time.Now()
	observation.observeContentDelta(responseStreamEvent{Delta: responseContentBlock{Type: "signature_delta"}})
	if observation.timeToFirstToken(started) != 0 {
		t.Fatal("unrecognized delta recorded token time")
	}
	observation.observeContentDelta(responseStreamEvent{Delta: responseContentBlock{Type: "text_delta", Text: "private first token"}})
	first := observation.firstTokenAt
	if first.IsZero() || observation.timeToFirstToken(started) <= 0 {
		t.Fatal("first token not recorded")
	}
	observation.observeContentDelta(responseStreamEvent{Delta: responseContentBlock{Type: "text_delta", Text: "private second token"}})
	if !observation.firstTokenAt.Equal(first) {
		t.Fatal("first token time overwritten")
	}
}

func TestModelProxyTelemetry(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meters := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	traces := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	oldMeter, oldTrace := otel.GetMeterProvider(), otel.GetTracerProvider()
	otel.SetMeterProvider(meters)
	otel.SetTracerProvider(traces)
	t.Cleanup(func() {
		otel.SetMeterProvider(oldMeter)
		otel.SetTracerProvider(oldTrace)
		_ = meters.Shutdown(context.Background())
		_ = traces.Shutdown(context.Background())
	})
	for _, tc := range []struct {
		name                           string
		upstreamStatus, responseStatus int
		errorType, body                string
		closed                         bool
	}{
		{"transport", 200, 502, "transport_error", "", true},
		{"http", 500, 500, "http_error", "private upstream failure", false},
		{"stream", 200, 200, "", "data: {\"type\":\"message_stop\"}\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("X-Upstream-Test", "preserved")
				w.WriteHeader(tc.upstreamStatus)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := server.Client()
			if tc.closed {
				server.Close()
			}
			call := startModelCall(t.Context(), auth.Principal{}, tc.name)
			incoming := httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(call.ctx)
			upstream, err := http.NewRequestWithContext(call.ctx, http.MethodPost, server.URL, strings.NewReader(`{"private":"input"}`))
			if err != nil {
				t.Fatal(err)
			}
			handler := &Handler{client: client, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			response := httptest.NewRecorder()
			handler.proxyModelRequest(response, incoming, upstream, nil, call)
			call.end()
			if response.Code != tc.responseStatus {
				t.Fatalf("status = %d, want %d", response.Code, tc.responseStatus)
			}
			if !tc.closed && (response.Body.String() != tc.body || response.Header().Get("X-Upstream-Test") != "preserved") {
				t.Fatal("proxy changed response")
			}
			spans := recorder.Ended()
			span := spans[len(spans)-1]
			if span.Name() != "chat "+tc.name || len(span.Events()) != 0 {
				t.Fatalf("model span: %s", span.Name())
			}
			for _, a := range span.Attributes() {
				if strings.Contains(a.Value.Emit(), "private") {
					t.Fatal("payload leaked to span")
				}
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name != "gen_ai.client.operation.duration" {
						continue
					}
					for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
						model, _ := point.Attributes.Value(attribute.Key("gen_ai.request.model"))
						if model.AsString() != tc.name {
							continue
						}
						found = true
						errorType, _ := point.Attributes.Value(attribute.Key("error.type"))
						status, _ := point.Attributes.Value(attribute.Key("http.response.status_code"))
						wantStatus := tc.upstreamStatus
						if tc.closed {
							wantStatus = 0
						}
						if point.Count != 1 || errorType.AsString() != tc.errorType || status.AsInt64() != int64(wantStatus) {
							t.Fatalf("operation metric: %+v", point)
						}
					}
				}
			}
			if !found {
				t.Fatal("operation duration missing")
			}
		})
	}
}

func TestModelTokenUsagePreservesNilAndZero(t *testing.T) {
	if got := modelTokenUsage(codesessions.ModelRequestUsage{}); got.Input != nil || got.Output != nil || got.CacheRead != nil || got.CacheCreation != nil {
		t.Fatal("missing usage became measured")
	}
	zero := int64(0)
	got := modelTokenUsage(codesessions.ModelRequestUsage{OutputTokens: &zero})
	if got.Output == nil || *got.Output != 0 {
		t.Fatal("explicit zero token count lost")
	}
}
