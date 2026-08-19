package deployments

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseDeploymentScheduleAcceptsSundaySeven(t *testing.T) {
	schedule, err := parseDeploymentSchedule(json.RawMessage(`{"type":"cron","expression":"0 9 * * 7","timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("parseDeploymentSchedule() error = %v", err)
	}
	next := schedule.cron.Next(time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC))
	want := time.Date(2026, time.August, 23, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Schedule.Next() = %v, want %v", next, want)
	}
}

func TestParseDeploymentScheduleAcceptsSundaySevenInRange(t *testing.T) {
	schedule, err := parseDeploymentSchedule(json.RawMessage(`{"type":"cron","expression":"0 9 * * 5-7","timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("parseDeploymentSchedule() error = %v", err)
	}
	next := schedule.cron.Next(time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC))
	want := time.Date(2026, time.August, 23, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("Schedule.Next() = %v, want %v", next, want)
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
