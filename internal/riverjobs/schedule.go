package riverjobs

import "github.com/riverqueue/river/rivertype"

// MatchesActiveCron reports whether an existing periodic job already has the requested active schedule.
func MatchesActiveCron(job *rivertype.DurablePeriodicJob, kind, queue, cron, timezone string) bool {
	return job != nil && job.CronExpression != nil && *job.CronExpression == cron &&
		job.CronTimezone == timezone && job.Kind == kind && job.Queue == queue && job.PausedAt == nil
}
