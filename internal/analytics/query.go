package analytics

import (
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type Query struct {
	Data      db.UsageQuery
	Workspace string
}

func ParseQuery(values url.Values, now time.Time) (Query, error) {
	timezone := strings.TrimSpace(values.Get("timezone"))
	if timezone == "" {
		timezone = "UTC"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return Query{}, invalidQuery("timezone")
	}
	localNow := now.In(location)
	start := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	start, err = queryTime(values, "starting_at", "starting_on", start, location)
	if err != nil {
		return Query{}, err
	}
	end, err = queryTime(values, "ending_at", "ending_before", end, location)
	if err != nil {
		return Query{}, err
	}
	if !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
		return Query{}, invalidQuery("time_range")
	}
	source := values.Get("source")
	if source != "" && !slices.Contains([]string{"managed_agents", "api", "workbench"}, source) {
		return Query{}, invalidQuery("source")
	}
	if granularity := values.Get("granularity"); granularity != "" && granularity != "daily" {
		return Query{}, invalidQuery("granularity")
	}
	if key := strings.TrimSpace(values.Get("api_key_id")); key != "" {
		if _, err := uuid.Parse(key); err != nil {
			return Query{}, invalidQuery("api_key_id")
		}
	}
	return Query{Data: db.UsageQuery{Start: start, End: end, AsOf: now, Timezone: timezone, Model: strings.TrimSpace(values.Get("model")), Source: source, SessionID: strings.TrimSpace(values.Get("session_id")), APIKeyUUID: strings.TrimSpace(values.Get("api_key_id"))}, Workspace: strings.TrimSpace(values.Get("workspace_id"))}, nil
}

func queryTime(values url.Values, timestampKey, dateKey string, fallback time.Time, location *time.Location) (time.Time, error) {
	if raw := values.Get(timestampKey); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, invalidQuery(timestampKey)
		}
		return value, nil
	}
	if raw := values.Get(dateKey); raw != "" {
		value, err := time.ParseInLocation(time.DateOnly, raw, location)
		if err != nil {
			return time.Time{}, invalidQuery(dateKey)
		}
		return value, nil
	}
	return fallback, nil
}
