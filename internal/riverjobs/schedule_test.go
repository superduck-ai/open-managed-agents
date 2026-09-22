package riverjobs

import (
	"github.com/riverqueue/river/rivertype"
	"testing"
	"time"
)

func TestMatchesActiveCron(t *testing.T) {
	cron := "*/5 * * * *"
	expected := rivertype.DurablePeriodicJob{Kind: "sweep", Queue: "archive", CronExpression: &cron, CronTimezone: "UTC"}
	for _, tc := range []struct {
		name   string
		mutate func(*rivertype.DurablePeriodicJob)
	}{
		{"non_cron", func(j *rivertype.DurablePeriodicJob) { j.CronExpression = nil }},
		{"cron", func(j *rivertype.DurablePeriodicJob) { other := "* * * * *"; j.CronExpression = &other }},
		{"timezone", func(j *rivertype.DurablePeriodicJob) { j.CronTimezone = "Asia/Shanghai" }},
		{"kind", func(j *rivertype.DurablePeriodicJob) { j.Kind = "other" }},
		{"queue", func(j *rivertype.DurablePeriodicJob) { j.Queue = "other" }},
		{"paused", func(j *rivertype.DurablePeriodicJob) { now := time.Now(); j.PausedAt = &now }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := expected
			tc.mutate(&job)
			if MatchesActiveCron(&job, "sweep", "archive", cron, "UTC") {
				t.Fatal("changed schedule matched")
			}
		})
	}
	if MatchesActiveCron(nil, "sweep", "archive", cron, "UTC") {
		t.Fatal("missing job matched")
	}
	if !MatchesActiveCron(&expected, "sweep", "archive", cron, "UTC") {
		t.Fatal("unchanged schedule must preserve existing job")
	}
}
