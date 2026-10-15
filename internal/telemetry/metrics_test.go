package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func metricReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	instrumentsOnce = sync.Once{}
	registry = instruments{}
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		instrumentsOnce = sync.Once{}
		registry = instruments{}
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

func collectedMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &data); err != nil {
		t.Fatal(err)
	}
	result := map[string]metricdata.Aggregation{}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			result[measurement.Name] = measurement.Data
		}
	}
	return result
}

func TestHTTPRequestMetric(t *testing.T) {
	reader := metricReader(t)
	if len(collectedMetrics(t, reader)) != 0 {
		t.Fatal("empty registry produced measurements")
	}
	RecordHTTPRequest(t.Context(), "POST", "/v1/sessions/{session_id}/events", 503, 2*time.Second)
	data := collectedMetrics(t, reader)
	histogram, ok := data["http.server.request.duration"].(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 {
		t.Fatalf("unexpected histogram: %v", data)
	}
	point := histogram.DataPoints[0]
	if point.Count != 1 || point.Sum != 2 || point.Attributes.Len() != 3 {
		t.Fatalf("unexpected measurement: %+v", point)
	}
	for key, want := range map[string]string{"http.request.method": "POST", "http.route": "/v1/sessions/{session_id}/events", "http.response.status_code": "503"} {
		found := false
		for _, attr := range point.Attributes.ToSlice() {
			if string(attr.Key) == key {
				found = attr.Value.Emit() == want
			}
		}
		if !found {
			t.Fatalf("attribute %s missing or incorrect", key)
		}
	}
}
