package telemetry

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestModelMetricsOmitMissingMeasurements(t *testing.T) {
	reader := metricReader(t)
	RecordModelCall(t.Context(), ModelCallMeasurement{Model: "model", Source: "api_key"})
	RecordModelCall(t.Context(), ModelCallMeasurement{Model: "model", Source: "code_session", Duration: -time.Second, TimeToFirstByte: -time.Second, TimeToFirstToken: -time.Second})
	if data := collectedMetrics(t, reader); len(data) != 0 {
		t.Fatalf("unmeasured metrics: %v", data)
	}
}

func TestModelMetricNamesAttributesAndUsage(t *testing.T) {
	reader := metricReader(t)
	RecordModelCall(t.Context(), ModelCallMeasurement{Model: "model", Source: "api_key", Status: 503, ErrorType: "http_error", Duration: time.Second, TimeToFirstByte: time.Second / 2})
	data := collectedMetrics(t, reader)
	failure := data["gen_ai.client.operation.duration"].(metricdata.Histogram[float64]).DataPoints[0]
	if failure.Count != 1 || failure.Sum != 1 || failure.Attributes.Len() != 4 {
		t.Fatalf("failure metric: %+v", failure)
	}
	for key, want := range map[string]string{"gen_ai.request.model": "model", "oma.source": "api_key", "http.response.status_code": "503", "error.type": "http_error"} {
		v, ok := failure.Attributes.Value(attribute.Key(key))
		if !ok || v.Emit() != want {
			t.Fatalf("%s = %v", key, v)
		}
	}
	input, output, cacheRead, cacheCreation := int64(12), int64(0), int64(3), int64(2)
	RecordModelCall(t.Context(), ModelCallMeasurement{Model: "model", Source: "code_session", Status: 200, Duration: 2 * time.Second, TimeToFirstByte: time.Second, TimeToFirstToken: 1500 * time.Millisecond, Usage: TokenUsage{Input: &input, Output: &output, CacheRead: &cacheRead, CacheCreation: &cacheCreation}})
	data = collectedMetrics(t, reader)
	if len(data) != 4 {
		t.Fatalf("metric names: %v", data)
	}
	durations := data["gen_ai.client.operation.duration"].(metricdata.Histogram[float64]).DataPoints
	found := false
	for _, point := range durations {
		status, _ := point.Attributes.Value(attribute.Key("http.response.status_code"))
		if status.AsInt64() == 200 {
			found = true
			if point.Count != 1 || point.Sum != 2 || point.Attributes.Len() != 3 {
				t.Fatalf("successful duration: %+v", point)
			}
		}
	}
	if !found {
		t.Fatal("success duration missing")
	}
	ttfb := data["oma.model.time_to_first_byte"].(metricdata.Histogram[float64]).DataPoints
	if len(ttfb) != 2 {
		t.Fatalf("TTFB: %v", ttfb)
	}
	for _, point := range ttfb {
		if point.Attributes.Len() != 2 || point.Count != 1 {
			t.Fatalf("TTFB attributes: %+v", point)
		}
	}
	ttft := data["oma.model.time_to_first_token"].(metricdata.Histogram[float64]).DataPoints
	if len(ttft) != 1 || ttft[0].Count != 1 || ttft[0].Sum != 1.5 || ttft[0].Attributes.Len() != 1 {
		t.Fatalf("TTFT: %v", ttft)
	}
	usage := data["gen_ai.client.token.usage"].(metricdata.Histogram[int64]).DataPoints
	if len(usage) != 4 {
		t.Fatalf("token measurements: %v", usage)
	}
	expected := map[string]int64{"input": 12, "output": 0, "cache_read": 3, "cache_creation": 2}
	for _, point := range usage {
		kind, _ := point.Attributes.Value(attribute.Key("gen_ai.token.type"))
		want, ok := expected[kind.AsString()]
		if !ok || point.Count != 1 || point.Sum != want || point.Attributes.Len() != 2 {
			t.Fatalf("token measurement: %+v", point)
		}
	}
}
