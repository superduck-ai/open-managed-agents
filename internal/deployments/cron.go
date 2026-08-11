package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
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
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, nil
	}
	schedule, err := parseDeploymentSchedule(raw)
	if err != nil {
		return nil, err
	}
	return jsonx.Encode(schedule.config)
}

func parseDeploymentSchedule(raw json.RawMessage) (parsedSchedule, error) {
	config, err := jsonx.Decode[deploymentSchedule](raw)
	if err != nil {
		return parsedSchedule{}, errors.New("schedule must be an object or null")
	}
	if config.Type != "cron" {
		return parsedSchedule{}, errors.New("schedule.type must be cron")
	}
	cronSchedule, err := cronParser.Parse("CRON_TZ=" + config.Timezone + " " + config.Expression)
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("schedule.expression %w", err)
	}
	return parsedSchedule{config: config, cron: cronSchedule}, nil
}

func upcomingRuns(schedule cron.Schedule, now time.Time, inactive bool) []string {
	values := make([]string, 0, upcomingRunCount)
	if inactive {
		return values
	}
	next := schedule.Next(now).UTC()
	for range upcomingRunCount {
		if next.IsZero() {
			break
		}
		values = append(values, next.Format(time.RFC3339))
		next = schedule.Next(next).UTC()
	}
	return values
}
