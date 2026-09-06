// Package deploymentjobs defines the shared cron and River job contract for deployments.
package deploymentjobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow) //nolint:gochecknoglobals

type Schedule struct {
	Type       string `json:"type"`
	Expression string `json:"expression"`
	Timezone   string `json:"timezone"`
}

type ParsedSchedule struct {
	Config Schedule
	Cron   cron.Schedule
}

func Parse(raw json.RawMessage) (ParsedSchedule, error) {
	config, err := jsonx.Decode[Schedule](raw)
	if err != nil {
		return ParsedSchedule{}, errors.New("schedule must be an object or null")
	}
	if config.Type != "cron" {
		return ParsedSchedule{}, errors.New("schedule.type must be cron")
	}
	if strings.ContainsAny(config.Expression, "LW#?@") {
		return ParsedSchedule{}, errors.New("schedule.expression contains unsupported syntax")
	}
	config.Timezone = strings.TrimSpace(config.Timezone)
	if config.Timezone == "" || config.Timezone == "Local" {
		return ParsedSchedule{}, errors.New("schedule.timezone must be a valid IANA timezone")
	}
	if _, err := time.LoadLocation(config.Timezone); err != nil {
		return ParsedSchedule{}, errors.New("schedule.timezone must be a valid IANA timezone")
	}
	cronSchedule, err := cronParser.Parse("CRON_TZ=" + config.Timezone + " " + mapSundaySeven(config.Expression))
	if err != nil {
		return ParsedSchedule{}, fmt.Errorf("schedule.expression %w", err)
	}
	return ParsedSchedule{Config: config, Cron: cronSchedule}, nil
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
