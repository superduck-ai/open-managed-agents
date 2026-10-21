package httpapi

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

func TestObserveSSEStream(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	closeStream := ObserveSSEStream(t.Context(), "public")
	data := telemetrytest.Collect(t, reader)
	telemetrytest.AssertSingle(t, data, "oma.sse.streams", 1, map[string]string{"oma.stream.kind": "public"})
	closeStream()
	data = telemetrytest.Collect(t, reader)
	if point := telemetrytest.Single(t, data, "oma.sse.streams"); point.Value != 0 {
		t.Fatalf("stream count did not return to zero: %+v", point)
	}
	telemetrytest.AssertSingle(t, data, "oma.sse.stream.duration", 0, map[string]string{"oma.stream.kind": "public"})
}
