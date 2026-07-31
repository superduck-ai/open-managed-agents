package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	enqueueWebhookDeliveryJobQuery = `
		insert into jobs (external_id, workspace_uuid, type, status, payload)
		values (
			concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_uuid,
			'webhook_delivery',
			'pending',
			CAST(:payload AS jsonb)
		)
	`
	enqueueWebhookDeliveryJobForEndpointQuery = `
		insert into jobs (external_id, workspace_uuid, type, status, payload)
		values (
			concat('job_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_uuid,
			'webhook_delivery',
			'pending',
			CAST(:payload AS jsonb)
		)
	`
	leaseWebhookDeliveryJobsQuery = `
		with next_jobs as (
			select uuid
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
			where j.uuid = next_jobs.uuid
			returning j.uuid, j.external_id, j.workspace_uuid, j.payload, j.attempts
		)
		select
			u.uuid,
			u.external_id,
			u.workspace_uuid,
			coalesce(u.payload->>'event_type', '') AS event_type,
			coalesce(u.payload->'event', CAST('{}' AS jsonb)) AS event,
			u.attempts,
			we.uuid AS webhook_endpoint_uuid,
			we.external_id AS webhook_endpoint_external_id,
			we.url AS webhook_endpoint_url,
			we.signing_secret AS webhook_endpoint_secret,
			we.status AS webhook_endpoint_status
		from updated_jobs u
		left join webhook_endpoints we
			on we.uuid = CAST(nullif(u.payload->>'webhook_endpoint_uuid', '') AS uuid)
			and we.deleted_at is null
	`
	completeWebhookDeliveryJobQuery = `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where uuid = :job_uuid and type = 'webhook_delivery'
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
		where uuid = :job_uuid and type = 'webhook_delivery'
	`
)

type WebhookDeliveryJob struct {
	UUID                      string
	ExternalID                string
	WorkspaceUUID             string
	EventType                 string
	Event                     json.RawMessage
	Attempts                  int
	WebhookEndpointUUID       *string
	WebhookEndpointExternalID string
	WebhookEndpointURL        string
	WebhookEndpointSecret     string
	WebhookEndpointStatus     string
}

type webhookDeliveryJobPayload struct {
	EventType           string          `json:"event_type"`
	Event               json.RawMessage `json:"event"`
	WebhookEndpointUUID string          `json:"webhook_endpoint_uuid,omitempty"`
}

type webhookDeliveryJobRow struct {
	UUID                      uuid.UUID      `db:"uuid"`
	ExternalID                string         `db:"external_id"`
	WorkspaceUUID             uuid.UUID      `db:"workspace_uuid"`
	EventType                 string         `db:"event_type"`
	Event                     []byte         `db:"event"`
	Attempts                  int            `db:"attempts"`
	WebhookEndpointUUID       uuid.NullUUID  `db:"webhook_endpoint_uuid"`
	WebhookEndpointExternalID sql.NullString `db:"webhook_endpoint_external_id"`
	WebhookEndpointURL        sql.NullString `db:"webhook_endpoint_url"`
	WebhookEndpointSecret     sql.NullString `db:"webhook_endpoint_secret"`
	WebhookEndpointStatus     sql.NullString `db:"webhook_endpoint_status"`
}

func (d *DB) EnqueueWebhookDeliveryJob(ctx context.Context, workspaceUUID, eventType string, event json.RawMessage) error {
	payload, err := json.Marshal(webhookDeliveryJobPayload{EventType: eventType, Event: event})
	if err != nil {
		return err
	}
	_, err = namedExecContext(ctx, d.sql, enqueueWebhookDeliveryJobQuery, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"payload":        payload,
	})
	return err
}

func (d *DB) EnqueueWebhookDeliveryJobForEndpoint(ctx context.Context, workspaceUUID, eventType string, event json.RawMessage, endpointUUID string) error {
	parsedEndpointUUID, err := parseDBUUID("webhook_endpoint_uuid", endpointUUID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(webhookDeliveryJobPayload{
		EventType:           eventType,
		Event:               event,
		WebhookEndpointUUID: parsedEndpointUUID.String(),
	})
	if err != nil {
		return err
	}
	_, err = namedExecContext(ctx, d.sql, enqueueWebhookDeliveryJobForEndpointQuery, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"payload":        payload,
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

func (d *DB) CompleteWebhookDeliveryJob(ctx context.Context, jobUUID string) error {
	_, err := namedExecContext(ctx, d.sql, completeWebhookDeliveryJobQuery, map[string]any{
		"job_uuid": dbUUID(jobUUID),
	})
	return err
}

func (d *DB) FailWebhookDeliveryJob(ctx context.Context, jobUUID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := namedExecContext(ctx, d.sql, failWebhookDeliveryJobQuery, map[string]any{
		"job_uuid":  dbUUID(jobUUID),
		"status":    status,
		"run_after": runAfter,
		"attempts":  nextAttempts,
		"reason":    reason,
	})
	return err
}

func (r webhookDeliveryJobRow) job() WebhookDeliveryJob {
	job := WebhookDeliveryJob{
		UUID:                      r.UUID.String(),
		ExternalID:                r.ExternalID,
		WorkspaceUUID:             r.WorkspaceUUID.String(),
		EventType:                 r.EventType,
		Event:                     copyRaw(r.Event),
		Attempts:                  r.Attempts,
		WebhookEndpointExternalID: r.WebhookEndpointExternalID.String,
		WebhookEndpointURL:        r.WebhookEndpointURL.String,
		WebhookEndpointSecret:     r.WebhookEndpointSecret.String,
		WebhookEndpointStatus:     r.WebhookEndpointStatus.String,
	}
	if r.WebhookEndpointUUID.Valid {
		endpointUUID := r.WebhookEndpointUUID.UUID.String()
		job.WebhookEndpointUUID = &endpointUUID
	}
	return job
}
