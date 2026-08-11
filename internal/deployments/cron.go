package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const upcomingRunCount = 5

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow) //nolint:gochecknoglobals

type deploymentSchedule struct {
	Type       string `json:"type"`
	Expression string `json:"expression"`
	Timezone   string `json:"timezone"`
}

type parsedSchedule struct {
	config deploymentSchedule
	cron   cron.Schedule
}

func normalizeOptionalSchedule(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	schedule, err := parseDeploymentSchedule(raw)
	if err != nil {
		return nil, err
	}
	return httpapi.MarshalRaw(schedule.config)
}

func parseDeploymentSchedule(raw json.RawMessage) (parsedSchedule, error) {
	var config deploymentSchedule
	if err := json.Unmarshal(raw, &config); err != nil {
		return parsedSchedule{}, errors.New("schedule must be an object or null")
	}
	if config.Type != "cron" {
		return parsedSchedule{}, errors.New("schedule.type must be cron")
	}
	config.Expression = strings.TrimSpace(config.Expression)
	if config.Expression == "" {
		return parsedSchedule{}, errors.New("schedule.expression is required")
	}
	config.Timezone = strings.TrimSpace(config.Timezone)
	if config.Timezone == "" {
		return parsedSchedule{}, errors.New("schedule.timezone is required")
	}
	cronSchedule, err := cronParser.Parse("CRON_TZ=" + config.Timezone + " " + config.Expression)
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("schedule.expression %w", err)
	}
	return parsedSchedule{config: config, cron: cronSchedule}, nil
}

func upcomingRuns(scheduleRaw json.RawMessage, now time.Time, archived bool) json.RawMessage {
	if len(scheduleRaw) == 0 || httpapi.IsJSONNull(scheduleRaw) || archived {
		return nil
	}
	schedule, err := parseDeploymentSchedule(scheduleRaw)
	if err != nil {
		return nil
	}
	values := make([]string, 0, upcomingRunCount)
	next := schedule.cron.Next(now).UTC()
	for range upcomingRunCount {
		if next.IsZero() {
			break
		}
		values = append(values, next.Format(time.RFC3339))
		next = schedule.cron.Next(next).UTC()
	}
	raw, _ := httpapi.MarshalRaw(values)
	return raw
}
