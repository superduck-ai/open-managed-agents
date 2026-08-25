package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	if strings.ContainsAny(config.Expression, "LW#?@") {
		return parsedSchedule{}, errors.New("schedule.expression contains unsupported syntax")
	}
	config.Timezone = strings.TrimSpace(config.Timezone)
	if config.Timezone == "" || config.Timezone == "Local" {
		return parsedSchedule{}, errors.New("schedule.timezone must be a valid IANA timezone")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return parsedSchedule{}, errors.New("schedule.timezone must be a valid IANA timezone")
	}
	cronSchedule, err := cronParser.Parse("CRON_TZ=" + config.Timezone + " " + mapSundaySeven(config.Expression))
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("schedule.expression %w", err)
	}
	return parsedSchedule{config: config, cron: cronSchedule}, nil
}

// cron/v3 only accepts DOW 0-6. Official Claude cron uses 0-7, where 7 is Sunday.
func mapSundaySeven(expression string) string {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return expression
	}
	items := strings.Split(fields[4], ",")
	for i, item := range items {
		switch {
		case item == "7":
			items[i] = "0"
		case strings.HasSuffix(item, "-7"):
			items[i] = strings.TrimSuffix(item, "-7") + "-6,0"
		}
	}
	fields[4] = strings.Join(items, ",")
	return strings.Join(fields, " ")
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
