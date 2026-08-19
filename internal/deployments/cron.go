package deployments

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	cronSchedule, err := cronParser.Parse("CRON_TZ=" + config.Timezone + " " + normalizeSundayAlias(config.Expression))
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("schedule.expression %w", err)
	}
	return parsedSchedule{config: config, cron: cronSchedule}, nil
}

func normalizeSundayAlias(expression string) string {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return expression
	}
	items := strings.Split(fields[4], ",")
	normalized := make([]string, 0, len(items)+1)
	for _, item := range items {
		base, stepText, hasStep := strings.Cut(item, "/")
		if base == "7" {
			normalized = append(normalized, "0")
			continue
		}
		startText, endText, hasRange := strings.Cut(base, "-")
		if !hasRange || endText != "7" {
			normalized = append(normalized, item)
			continue
		}
		start, startErr := strconv.Atoi(startText)
		step := 1
		var stepErr error
		if hasStep {
			step, stepErr = strconv.Atoi(stepText)
		}
		if startErr != nil || stepErr != nil || start > 7 || step <= 0 {
			normalized = append(normalized, item)
			continue
		}
		if start < 7 {
			rangeItem := startText + "-6"
			if hasStep {
				rangeItem += "/" + stepText
			}
			normalized = append(normalized, rangeItem)
		}
		if (7-start)%step == 0 {
			normalized = append(normalized, "0")
		}
	}
	fields[4] = strings.Join(normalized, ",")
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
