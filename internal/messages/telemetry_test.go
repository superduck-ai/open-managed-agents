package messages

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestModelObservationErrorTypes(t *testing.T) {
	for _, input := range []string{"invalid_response", "incomplete_response", "provider_error", "transport_error", "http_error", "stream_error", "cancelled", "observation_limit", ""} {
		want := input
		if input == "invalid_response" || input == "incomplete_response" || input == "provider_error" {
			want = "stream_error"
		}
		if input == "cancelled" {
			want = "canceled"
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
	reader := telemetrytest.MetricReader(t)
	recorder := telemetrytest.SpanRecorder(t)
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
			found := false
			for _, point := range telemetrytest.Points(t, telemetrytest.Collect(t, reader), "gen_ai.client.operation.duration") {
				model, _ := point.Attributes.Value(attribute.Key("gen_ai.request.model"))
				if model.AsString() != tc.name {
					continue
				}
				found = true
				errorType, _ := point.Attributes.Value(attribute.Key("error.type"))
				status, hasStatus := point.Attributes.Value(attribute.Key("http.response.status_code"))
				if hasStatus == tc.closed || (hasStatus && status.AsInt64() != int64(tc.upstreamStatus)) {
					t.Fatalf("operation status attribute: %+v", point)
				}
				if point.Count != 1 || errorType.AsString() != tc.errorType {
					t.Fatalf("operation metric: %+v", point)
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

func TestModelResponseAttributes(t *testing.T) {
	for _, tc := range []struct {
		name, stopReason string
		want             map[string]string
		absent           []string
	}{
		{"unknown stop reason", "private_reason", map[string]string{"gen_ai.response.finish_reasons": `["other"]`}, []string{"gen_ai.usage.input_tokens"}},
		{"missing observation fields", "", nil, []string{"gen_ai.response.finish_reasons", "oma.model.time_to_first_token_s", "oma.model.upstream_request_id"}},
		{"complete", "tool_use", map[string]string{
			"gen_ai.response.finish_reasons": `["tool_use"]`, "gen_ai.usage.output_tokens": "0", "gen_ai.usage.input_tokens": "12",
			"oma.model.time_to_first_token_s": "1.5", "oma.model.upstream_request_id": "req_upstream",
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation := &responseObservation{stopReason: tc.stopReason}
			measurement := telemetryMeasurementFor(tc.name)
			if tc.name == "complete" {
				observation.result.UpstreamRequestID = "req_upstream"
			}
			attrs := map[string]string{}
			for _, attr := range modelResponseAttributes(observation, measurement) {
				attrs[string(attr.Key)] = attr.Value.Emit()
			}
			for key, value := range tc.want {
				if attrs[key] != value {
					t.Fatalf("%s = %q, attrs %v", key, attrs[key], attrs)
				}
			}
			for _, key := range tc.absent {
				if _, ok := attrs[key]; ok {
					t.Fatalf("unexpected %s in %v", key, attrs)
				}
			}
			if strings.Contains(attrs["gen_ai.response.finish_reasons"], "private") {
				t.Fatal("unbounded stop reason leaked")
			}
		})
	}
}

func telemetryMeasurementFor(name string) modelCallMeasurement {
	if name != "complete" {
		return modelCallMeasurement{}
	}
	input, output := int64(12), int64(0)
	return modelCallMeasurement{
		Usage:            tokenUsage{Input: &input, Output: &output},
		TimeToFirstToken: 1500 * time.Millisecond,
	}
}
