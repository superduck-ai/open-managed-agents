package telemetrytest

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func MetricReader(t testing.TB) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

func SpanRecorder(t testing.TB) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return recorder
}

func Collect(t testing.TB, reader *sdkmetric.ManualReader) map[string]metricdata.Aggregation {
	t.Helper()
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
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

type Point struct {
	Value      float64
	Count      uint64
	Attributes attribute.Set
}

func Points(t testing.TB, data map[string]metricdata.Aggregation, name string) []Point {
	t.Helper()
	var points []Point
	switch aggregation := data[name].(type) {
	case metricdata.Sum[int64]:
		for _, point := range aggregation.DataPoints {
			points = append(points, Point{Value: float64(point.Value), Count: 1, Attributes: point.Attributes})
		}
	case metricdata.Histogram[float64]:
		for _, point := range aggregation.DataPoints {
			points = append(points, Point{Value: point.Sum, Count: point.Count, Attributes: point.Attributes})
		}
	case metricdata.Histogram[int64]:
		for _, point := range aggregation.DataPoints {
			points = append(points, Point{Value: float64(point.Sum), Count: point.Count, Attributes: point.Attributes})
		}
	case nil:
		t.Fatalf("metric %s not recorded: %v", name, data)
	default:
		t.Fatalf("metric %s has unsupported aggregation %T", name, aggregation)
	}
	return points
}

func Single(t testing.TB, data map[string]metricdata.Aggregation, name string) Point {
	t.Helper()
	points := Points(t, data, name)
	if len(points) != 1 {
		t.Fatalf("metric %s points = %d, want 1: %v", name, len(points), points)
	}
	return points[0]
}

func AssertAttributes(t testing.TB, name string, got attribute.Set, want map[string]string) {
	t.Helper()
	if got.Len() != len(want) {
		t.Fatalf("%s attributes = %v, want %v", name, got.ToSlice(), want)
	}
	for key, value := range want {
		actual, ok := got.Value(attribute.Key(key))
		if !ok || actual.Emit() != value {
			t.Fatalf("%s attribute %s = %v, want %s", name, key, actual.Emit(), value)
		}
	}
}

func AssertSingle(t testing.TB, data map[string]metricdata.Aggregation, name string, value float64, attrs map[string]string) {
	t.Helper()
	point := Single(t, data, name)
	if point.Count != 1 || point.Value < value || point.Value > value+0.5 {
		t.Fatalf("%s point = %+v, want value %v", name, point, value)
	}
	AssertAttributes(t, name, point.Attributes, attrs)
}
