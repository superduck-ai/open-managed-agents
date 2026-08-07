package deployments

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNextScheduledTimesHandlesLeapDay(t *testing.T) {
	times, err := nextScheduledTimes(
		json.RawMessage(`{"type":"cron","expression":"0 0 29 2 *","timezone":"UTC"}`),
		time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC),
		1,
	)
	if err != nil {
		t.Fatalf("nextScheduledTimes() error = %v", err)
	}
	want := time.Date(2028, time.February, 29, 0, 0, 0, 0, time.UTC)
	if !times[0].Equal(want) {
		t.Fatalf("nextScheduledTimes() = %v, want %v", times[0], want)
	}
}

func TestNextScheduledTimesHandlesDST(t *testing.T) {
	t.Run("spring forward skips nonexistent wall time", func(t *testing.T) {
		times, err := nextScheduledTimes(
			json.RawMessage(`{"type":"cron","expression":"30 2 * * *","timezone":"America/New_York"}`),
			time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC),
			2,
		)
		if err != nil {
			t.Fatalf("nextScheduledTimes() error = %v", err)
		}
		if times[0].Day() != 7 || times[1].Day() != 9 {
			t.Fatalf("nextScheduledTimes() days = %d, %d, want 7, 9", times[0].Day(), times[1].Day())
		}
	})

	t.Run("fall back repeats wall time", func(t *testing.T) {
		times, err := nextScheduledTimes(
			json.RawMessage(`{"type":"cron","expression":"30 1 * * *","timezone":"America/New_York"}`),
			time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC),
			3,
		)
		if err != nil {
			t.Fatalf("nextScheduledTimes() error = %v", err)
		}
		if times[2].Sub(times[1]) != time.Hour {
			t.Fatalf("repeated wall times differ by %v, want 1h", times[2].Sub(times[1]))
		}
	})
}

func TestNormalizeOptionalScheduleRejectsUnsupportedSyntax(t *testing.T) {
	tests := []string{
		`{"type":"cron","expression":"@daily","timezone":"UTC"}`,
		`{"type":"cron","expression":"0 0 L * *","timezone":"UTC"}`,
		`{"type":"cron","expression":"0 0 0 * * *","timezone":"UTC"}`,
	}
	for _, raw := range tests {
		if _, err := normalizeOptionalSchedule(json.RawMessage(raw)); err == nil {
			t.Fatalf("normalizeOptionalSchedule(%s) error = nil", raw)
		}
	}
}

func TestNormalizeOptionalScheduleAcceptsSundaySeven(t *testing.T) {
	_, err := normalizeOptionalSchedule(json.RawMessage(`{"type":"cron","expression":"0 0 * * 7","timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("normalizeOptionalSchedule() error = %v", err)
	}
}

func TestJitteredTriggerAtIsStableAndBounded(t *testing.T) {
	schedule := json.RawMessage(`{"type":"cron","expression":"0 * * * *","timezone":"UTC"}`)
	scheduledAt := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	first, err := jitteredTriggerAt("depl_test", schedule, scheduledAt)
	if err != nil {
		t.Fatalf("jitteredTriggerAt() error = %v", err)
	}
	second, err := jitteredTriggerAt("depl_test", schedule, scheduledAt)
	if err != nil {
		t.Fatalf("jitteredTriggerAt() second error = %v", err)
	}
	if !first.Equal(second) {
		t.Fatalf("jitteredTriggerAt() = %v then %v", first, second)
	}
	if first.Before(scheduledAt) || !first.Before(scheduledAt.Add(9*time.Minute)) {
		t.Fatalf("jitteredTriggerAt() = %v, want [%v, %v)", first, scheduledAt, scheduledAt.Add(9*time.Minute))
	}
}
