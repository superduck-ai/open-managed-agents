package deployments

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpcomingRuns(t *testing.T) {
	schedule, err := parseDeploymentSchedule(json.RawMessage(`{"type":"cron","expression":"*/10 * * * *","timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("parse schedule: %v", err)
	}
	runs := upcomingRuns(
		schedule.cron,
		time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC),
		false,
	)
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

func TestParseDeploymentScheduleDoesNotRewriteInput(t *testing.T) {
	schedule, err := parseDeploymentSchedule(json.RawMessage(`{"type":"cron","expression":" */10 * * * *","timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("parse schedule: %v", err)
	}
	if schedule.config.Expression != " */10 * * * *" {
		t.Fatalf("expression = %q, want original input", schedule.config.Expression)
	}
}
