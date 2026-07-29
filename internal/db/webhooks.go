package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const (
	enqueueWebhookDeliveryJobQuery = `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_id,
			'webhook_delivery',
			'pending',
			jsonb_build_object(
				'event_type', CAST(:event_type AS text),
				'event', CAST(:event AS jsonb)
			)
		)
	`
	enqueueWebhookDeliveryJobForEndpointQuery = `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_id,
			'webhook_delivery',
			'pending',
			jsonb_build_object(
				'event_type', CAST(:event_type AS text),
				'event', CAST(:event AS jsonb),
				'webhook_endpoint_id', CAST(:webhook_endpoint_id AS bigint)
			)
		)
	`
	leaseWebhookDeliveryJobsQuery = `
		with next_jobs as (
			select id
			from jobs
			where type = 'webhook_delivery'
				and run_after <= now()
				and (
					status in ('pending', 'retry')
					or (status = 'running' and locked_until < now())
				)
			order by run_after, created_at
			limit :limit
			for update skip locked
		),
		updated_jobs as (
			update jobs j
			set status = 'running',
				locked_by = :worker_id,
				locked_until = now() + :lease_microseconds * interval '1 microsecond',
				updated_at = now()
			from next_jobs
			where j.id = next_jobs.id
			returning j.id, j.external_id, j.workspace_id, j.payload, j.attempts
		)
		select
			u.id,
			u.external_id,
			u.workspace_id,
			coalesce(u.payload->>'event_type', '') AS event_type,
			coalesce(u.payload->'event', CAST('{}' AS jsonb)) AS event,
			u.attempts,
			we.id AS webhook_endpoint_id,
			we.external_id AS webhook_endpoint_external_id,
			we.url AS webhook_endpoint_url,
			we.signing_secret AS webhook_endpoint_secret,
			we.status AS webhook_endpoint_status
		from updated_jobs u
		left join webhook_endpoints we
			on we.id = CAST(nullif(u.payload->>'webhook_endpoint_id', '') AS bigint)
			and we.deleted_at is null
	`
	completeWebhookDeliveryJobQuery = `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where id = :job_id and type = 'webhook_delivery'
	`
	failWebhookDeliveryJobQuery = `
		update jobs
		set status = :status,
			locked_by = null,
			locked_until = null,
			run_after = :run_after,
			updated_at = now(),
			attempts = :attempts,
			payload = payload || jsonb_build_object('last_error', CAST(:reason AS text))
		where id = :job_id and type = 'webhook_delivery'
	`
)

type WebhookDeliveryJob struct {
	ID                        int64
	ExternalID                string
	WorkspaceID               int64
	EventType                 string
	Event                     json.RawMessage
	Attempts                  int
	WebhookEndpointID         *int64
	WebhookEndpointExternalID string
	WebhookEndpointURL        string
	WebhookEndpointSecret     string
	WebhookEndpointStatus     string
}

type webhookDeliveryJobRow struct {
	ID                        int64          `db:"id"`
	ExternalID                string         `db:"external_id"`
	WorkspaceID               int64          `db:"workspace_id"`
	EventType                 string         `db:"event_type"`
	Event                     []byte         `db:"event"`
	Attempts                  int            `db:"attempts"`
	WebhookEndpointID         sql.NullInt64  `db:"webhook_endpoint_id"`
	WebhookEndpointExternalID sql.NullString `db:"webhook_endpoint_external_id"`
	WebhookEndpointURL        sql.NullString `db:"webhook_endpoint_url"`
	WebhookEndpointSecret     sql.NullString `db:"webhook_endpoint_secret"`
	WebhookEndpointStatus     sql.NullString `db:"webhook_endpoint_status"`
}

func (d *DB) EnqueueWebhookDeliveryJob(ctx context.Context, workspaceID int64, eventType string, event json.RawMessage) error {
	_, err := namedExecContext(ctx, d.sql, enqueueWebhookDeliveryJobQuery, map[string]any{
		"workspace_id": workspaceID,
		"event_type":   eventType,
		"event":        jsonArg(event),
	})
	return err
}

func (d *DB) EnqueueWebhookDeliveryJobForEndpoint(ctx context.Context, workspaceID int64, eventType string, event json.RawMessage, endpointID int64) error {
	_, err := namedExecContext(ctx, d.sql, enqueueWebhookDeliveryJobForEndpointQuery, map[string]any{
		"workspace_id":        workspaceID,
		"event_type":          eventType,
		"event":               jsonArg(event),
		"webhook_endpoint_id": endpointID,
	})
	return err
}

func (d *DB) LeaseWebhookDeliveryJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]WebhookDeliveryJob, error) {
	if limit <= 0 {
		limit = 10
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	var rows []webhookDeliveryJobRow
	if err := namedSelectContext(ctx, d.sql, &rows, leaseWebhookDeliveryJobsQuery, map[string]any{
		"limit":              limit,
		"worker_id":          workerID,
		"lease_microseconds": leaseDuration.Microseconds(),
	}); err != nil {
		return nil, err
	}

	jobs := make([]WebhookDeliveryJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

func (d *DB) CompleteWebhookDeliveryJob(ctx context.Context, jobID int64) error {
	_, err := namedExecContext(ctx, d.sql, completeWebhookDeliveryJobQuery, map[string]any{
		"job_id": jobID,
	})
	return err
}

func (d *DB) FailWebhookDeliveryJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := namedExecContext(ctx, d.sql, failWebhookDeliveryJobQuery, map[string]any{
		"job_id":    jobID,
		"status":    status,
		"run_after": runAfter,
		"attempts":  nextAttempts,
		"reason":    reason,
	})
	return err
}

func (r webhookDeliveryJobRow) job() WebhookDeliveryJob {
	job := WebhookDeliveryJob{
		ID:                        r.ID,
		ExternalID:                r.ExternalID,
		WorkspaceID:               r.WorkspaceID,
		EventType:                 r.EventType,
		Event:                     copyRaw(r.Event),
		Attempts:                  r.Attempts,
		WebhookEndpointExternalID: r.WebhookEndpointExternalID.String,
		WebhookEndpointURL:        r.WebhookEndpointURL.String,
		WebhookEndpointSecret:     r.WebhookEndpointSecret.String,
		WebhookEndpointStatus:     r.WebhookEndpointStatus.String,
	}
	if r.WebhookEndpointID.Valid {
		endpointID := r.WebhookEndpointID.Int64
		job.WebhookEndpointID = &endpointID
	}
	return job
}
