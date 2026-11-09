package telemetry

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/superduck-ai/open-managed-agents/internal/telemetry/telemetrytest"
)

var testCounters = NewInstruments(func(meter metric.Meter) metric.Int64Counter {
	return Counter(meter, "oma.test.counter", "{event}", "Test counter")
})

func TestInstrumentsFollowGlobalProvider(t *testing.T) {
	for range 2 {
		reader := telemetrytest.MetricReader(t)
		testCounters.Get().Add(t.Context(), 1)
		telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "oma.test.counter", 1, nil)
	}
}

func TestRecordWaitRejectsNegativeDurations(t *testing.T) {
	reader := telemetrytest.MetricReader(t)
	histogram := Histogram(otel.GetMeterProvider().Meter(InstrumentationName), "oma.test.wait", "s", "Test wait", LongWaitBuckets())
	RecordWait(t.Context(), histogram, -time.Second)
	if data := telemetrytest.Collect(t, reader); len(data) != 0 {
		t.Fatalf("negative wait recorded: %v", data)
	}
	RecordWait(t.Context(), histogram, 2*time.Second)
	telemetrytest.AssertSingle(t, telemetrytest.Collect(t, reader), "oma.test.wait", 2, nil)
}

func TestEnumAndToken(t *testing.T) {
	enum := NewEnum("end_turn", "requires_action")
	for input, want := range map[string]string{"end_turn": "end_turn", "Private Value": OtherValue, "": OtherValue} {
		if got := enum.Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{"stream_event": "stream_event", "control.response": "control.response", "Private Payload": OtherValue, "": OtherValue} {
		if got := Token(input); got != want {
			t.Fatalf("Token(%q) = %q, want %q", input, got, want)
		}
	}
}
