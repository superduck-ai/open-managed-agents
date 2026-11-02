//go:build integration

package tests

import (
	"context"
	"io"
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
)

func TestMessagesTelemetryEndToEnd(t *testing.T) {
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
	stream := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_telemetry\",\"usage\":{\"input_tokens\":12,\"output_tokens\":0,\"cache_read_input_tokens\":3,\"cache_creation_input_tokens\":2}}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"private model response\"}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Telemetry-Test") == "transport" {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		w.Header().Set("X-Upstream-Test", "preserved")
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.Header.Get("X-Telemetry-Test") {
		case "http":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "private upstream failure")
		case "invalid":
			_, _ = io.WriteString(w, "data: {broken}\n\n")
		default:
			_, _ = io.WriteString(w, stream)
		}
	}))
	defer upstream.Close()
	app := newTestAppWithStore(t, nil, newFakeStore("messages-telemetry"))
	defer app.close()
	clearTestLLMProviders(t, app)
	seedTestLLMProvider(t, app, "Telemetry", upstream.URL, "private-provider-key", messagesTestModel)
	credential := createMessagesCodeSessionCredential(t, app, messagesTestModel)
	registerCodeSessionWorker(t, app, credential.CodeSessionID)
	for _, tc := range []struct {
		name                   string
		status, upstreamStatus int
		errorType              string
	}{
		{"transport", 502, 0, "transport_error"},
		{"http", 500, 500, "http_error"},
		{"invalid", 200, 200, "stream_error"},
		{"success", 200, 200, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(recorder.Ended())
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(`{"model":"`+messagesTestModel+`","stream":true,"messages":[{"role":"user","content":"private model input"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Api-Key", credential.Token)
			request.Header.Set("X-Telemetry-Test", tc.name)
			response, err := app.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, tc.status, body)
			}
			if tc.name != "transport" && response.Header.Get("X-Upstream-Test") != "preserved" {
				t.Fatal("upstream header changed")
			}
			if tc.name == "success" && string(body) != stream {
				t.Fatal("stream bytes changed")
			}
			var modelSpan sdktrace.ReadOnlySpan
			deadline := time.Now().Add(5 * time.Second)
			for modelSpan == nil {
				for _, span := range recorder.Ended()[before:] {
					if span.Name() == "chat "+messagesTestModel {
						modelSpan = span
					}
				}
				if modelSpan == nil {
					if time.Now().After(deadline) {
						t.Fatal("model span missing after request finalization")
					}
					time.Sleep(time.Millisecond)
				}
			}
			if len(modelSpan.Events()) != 0 {
				t.Fatal("exception event recorded")
			}
			for _, a := range modelSpan.Attributes() {
				if strings.Contains(a.Value.Emit(), "private") || strings.Contains(a.Value.Emit(), credential.Token) {
					t.Fatal("sensitive content in model span")
				}
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			found := false
			ttfb, ttft := false, false
			tokens := map[string]int64{}
			for _, scope := range data.ScopeMetrics {
				for _, m := range scope.Metrics {
					switch m.Name {
					case "gen_ai.client.operation.duration":
						for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
							errorType, _ := point.Attributes.Value(attribute.Key("error.type"))
							status, _ := point.Attributes.Value(attribute.Key("http.response.status_code"))
							source, _ := point.Attributes.Value(attribute.Key("oma.source"))
							if errorType.AsString() == tc.errorType && status.AsInt64() == int64(tc.upstreamStatus) && source.AsString() == "code_session" {
								found = true
								if point.Count != 1 || point.Sum <= 0 {
									t.Fatalf("operation duration: %+v", point)
								}
							}
						}
					case "oma.model.time_to_first_byte":
						for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
							ttfb = ttfb || point.Count > 0 && point.Sum > 0
						}
					case "oma.model.time_to_first_token":
						for _, point := range m.Data.(metricdata.Histogram[float64]).DataPoints {
							ttft = ttft || point.Count == 1 && point.Sum > 0
						}
					case "gen_ai.client.token.usage":
						for _, point := range m.Data.(metricdata.Histogram[int64]).DataPoints {
							kind, _ := point.Attributes.Value(attribute.Key("gen_ai.token.type"))
							tokens[kind.AsString()] = point.Sum
						}
					}
				}
			}
			if !found {
				t.Fatal("operation metric status/error missing")
			}
			if tc.name == "success" {
				if !ttfb || !ttft {
					t.Fatalf("stream timing TTFB=%t TTFT=%t", ttfb, ttft)
				}
				for kind, want := range map[string]int64{"input": 12, "output": 7, "cache_read": 3, "cache_creation": 2} {
					if tokens[kind] != want {
						t.Fatalf("%s tokens=%d, want %d", kind, tokens[kind], want)
					}
				}
			}
		})
	}
}
