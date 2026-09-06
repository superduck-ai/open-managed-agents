package deployments

import (
	"encoding/json"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/superduck-ai/open-managed-agents/internal/common/jsonx"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
)

const upcomingRunCount = 5

func normalizeOptionalSchedule(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || jsonx.IsNull(raw) {
		return nil, nil
	}
	schedule, err := deploymentjobs.Parse(raw)
	if err != nil {
		return nil, err
	}
	return jsonx.Encode(schedule.Config)
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
