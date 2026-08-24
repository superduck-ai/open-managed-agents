package deployments

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseDeploymentScheduleAcceptsSunday(t *testing.T) {
	now := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	want := time.Date(2026, time.August, 23, 9, 0, 0, 0, time.UTC)
	for _, expression := range []string{"0 9 * * 0", "0 9 * * 7", "0 9 * * 5-7"} {
		schedule, err := parseDeploymentSchedule(json.RawMessage(`{"type":"cron","expression":"` + expression + `","timezone":"UTC"}`))
		if err != nil {
			t.Fatalf("parseDeploymentSchedule(%q) error = %v", expression, err)
		}
		if next := schedule.cron.Next(now); !next.Equal(want) {
			t.Fatalf("Schedule.Next(%q) = %v, want %v", expression, next, want)
		}
	}
}

func TestParseDeploymentScheduleRejectsUnsupportedSyntax(t *testing.T) {
	for _, expression := range []string{
		"0 9 ? * *",
		"0 9 L * *",
		"0 9 W * *",
		"0 9 * * 1#2",
		"@daily",
	} {
		raw := json.RawMessage(`{"type":"cron","expression":"` + expression + `","timezone":"UTC"}`)
		if _, err := parseDeploymentSchedule(raw); err == nil {
			t.Errorf("parseDeploymentSchedule(%q) error = nil", expression)
		}
	}
}

func TestParseDeploymentScheduleRequiresIANATimezone(t *testing.T) {
	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"type":"cron","expression":"0 9 * * *"}`),
		json.RawMessage(`{"type":"cron","expression":"0 9 * * *","timezone":""}`),
		json.RawMessage(`{"type":"cron","expression":"0 9 * * *","timezone":"  "}`),
		json.RawMessage(`{"type":"cron","expression":"0 9 * * *","timezone":"Local"}`),
	} {
		if _, err := parseDeploymentSchedule(raw); err == nil {
			t.Errorf("parseDeploymentSchedule(%s) error = nil", raw)
		}
	}
}

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
