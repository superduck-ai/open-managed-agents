package deployments

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpcomingRuns(t *testing.T) {
	raw := upcomingRuns(
		json.RawMessage(`{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`),
		time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
		false,
	)
	var runs []string
	if err := json.Unmarshal(raw, &runs); err != nil {
		t.Fatalf("unmarshal upcoming runs: %v", err)
	}
	if len(runs) != upcomingRunCount || runs[0] != "2026-08-11T00:10:00Z" {
		t.Fatalf("upcomingRuns() = %v", runs)
	}
}

func TestNormalizeOptionalScheduleRejectsUnsupportedSyntax(t *testing.T) {
	tests := []string{
		`{"type":"cron","expression":"bad","timezone":"UTC"}`,
		`{"type":"cron","expression":"0 0 * * *","timezone":"not/a-zone"}`,
	}
	for _, raw := range tests {
		if _, err := normalizeOptionalSchedule(json.RawMessage(raw)); err == nil {
			t.Fatalf("normalizeOptionalSchedule(%s) error = nil", raw)
		}
	}
}
