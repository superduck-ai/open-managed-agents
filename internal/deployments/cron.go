package deployments

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	config   deploymentSchedule
	cron     cron.Schedule
	location *time.Location
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
	if len(strings.Fields(config.Expression)) != 5 {
		return parsedSchedule{}, errors.New("schedule.expression must be a 5-field POSIX cron expression")
	}
	if strings.ContainsAny(config.Expression, "LW#?@") {
		return parsedSchedule{}, errors.New("schedule.expression contains unsupported syntax")
	}
	parts := strings.Fields(config.Expression)
	parts[4] = normalizeSundayAlias(parts[4])
	cronSchedule, err := cronParser.Parse(strings.Join(parts, " "))
	if err != nil {
		return parsedSchedule{}, fmt.Errorf("schedule.expression %w", err)
	}
	config.Timezone = strings.TrimSpace(config.Timezone)
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return parsedSchedule{}, errors.New("schedule.timezone must be a valid IANA timezone")
	}
	return parsedSchedule{config: config, cron: cronSchedule, location: location}, nil
}

func normalizeSundayAlias(field string) string {
	items := strings.Split(field, ",")
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
	return strings.Join(normalized, ",")
}

func nextScheduledAt(raw json.RawMessage, after time.Time) (*time.Time, error) {
	if len(raw) == 0 || httpapi.IsJSONNull(raw) {
		return nil, nil
	}
	schedule, err := parseDeploymentSchedule(raw)
	if err != nil {
		return nil, err
	}
	next := schedule.cron.Next(after.In(schedule.location)).UTC()
	return &next, nil
}

func nextScheduledTimes(raw json.RawMessage, after time.Time, count int) ([]time.Time, error) {
	schedule, err := parseDeploymentSchedule(raw)
	if err != nil {
		return nil, err
	}
	times := make([]time.Time, 0, count)
	cursor := schedule.cron.Next(after.In(schedule.location)).UTC()
	for range count {
		times = append(times, cursor)
		cursor = nextAfterOccurrence(schedule, cursor)
	}
	return times, nil
}

func nextAfterScheduled(raw json.RawMessage, scheduledAt time.Time) (*time.Time, error) {
	schedule, err := parseDeploymentSchedule(raw)
	if err != nil {
		return nil, err
	}
	next := nextAfterOccurrence(schedule, scheduledAt.UTC())
	return &next, nil
}

func nextAfterOccurrence(schedule parsedSchedule, occurrence time.Time) time.Time {
	local := occurrence.In(schedule.location)
	for candidate := occurrence.Add(time.Minute); !candidate.After(occurrence.Add(3 * time.Hour)); candidate = candidate.Add(time.Minute) {
		candidateLocal := candidate.In(schedule.location)
		if sameWallMinute(local, candidateLocal) {
			return candidate.UTC()
		}
	}
	return schedule.cron.Next(local).UTC()
}

func sameWallMinute(left, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day() &&
		left.Hour() == right.Hour() && left.Minute() == right.Minute()
}

func jitteredTriggerAt(deploymentID string, scheduleRaw json.RawMessage, scheduledAt time.Time) (time.Time, error) {
	next, err := nextAfterScheduled(scheduleRaw, scheduledAt)
	if err != nil {
		return time.Time{}, err
	}
	window := time.Duration(float64(next.Sub(scheduledAt)) * 0.15)
	window = max(window, 5*time.Second)
	window = min(window, 9*time.Minute)
	digest := sha256.Sum256([]byte(deploymentID + "\x00" + scheduledAt.UTC().Format(time.RFC3339Nano)))
	jitter := time.Duration(binary.BigEndian.Uint64(digest[:8]) % uint64(window))
	return scheduledAt.Add(jitter), nil
}

func upcomingRuns(scheduleRaw json.RawMessage, now time.Time, archived bool) json.RawMessage {
	if len(scheduleRaw) == 0 || httpapi.IsJSONNull(scheduleRaw) || archived {
		return nil
	}
	times, err := nextScheduledTimes(scheduleRaw, now, upcomingRunCount)
	if err != nil {
		return nil
	}
	values := make([]string, 0, len(times))
	for _, value := range times {
		values = append(values, value.Format(time.RFC3339))
	}
	raw, _ := httpapi.MarshalRaw(values)
	return raw
}
