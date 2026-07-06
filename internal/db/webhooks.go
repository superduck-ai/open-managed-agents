package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
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

func (d *DB) EnqueueWebhookDeliveryJob(ctx context.Context, workspaceID int64, eventType string, event json.RawMessage) error {
	_, err := d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1,
			'webhook_delivery',
			'pending',
			jsonb_build_object('event_type', $2::text, 'event', $3::jsonb)
		)
	`, workspaceID, eventType, jsonArg(event))
	return err
}

func (d *DB) EnqueueWebhookDeliveryJobForEndpoint(ctx context.Context, workspaceID int64, eventType string, event json.RawMessage, endpointID int64) error {
	_, err := d.Pool.Exec(ctx, `
		insert into jobs (external_id, workspace_id, type, status, payload)
		values (
			concat('job_', replace(gen_random_uuid()::text, '-', '')),
			$1,
			'webhook_delivery',
			'pending',
			jsonb_build_object(
				'event_type', $2::text,
				'event', $3::jsonb,
				'webhook_endpoint_id', $4::bigint
			)
		)
	`, workspaceID, eventType, jsonArg(event), endpointID)
	return err
}

func (d *DB) LeaseWebhookDeliveryJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]WebhookDeliveryJob, error) {
	if limit <= 0 {
		limit = 10
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	rows, err := d.Pool.Query(ctx, `
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
			limit $1
			for update skip locked
		),
		updated_jobs as (
		update jobs j
		set status = 'running',
			locked_by = $2,
			locked_until = now() + $3::interval,
			updated_at = now()
		from next_jobs
		where j.id = next_jobs.id
			returning j.id, j.external_id, j.workspace_id, j.payload, j.attempts
		)
		select u.id, u.external_id, u.workspace_id,
			coalesce(u.payload->>'event_type', ''),
			coalesce(u.payload->'event', '{}'::jsonb),
			u.attempts,
			we.id,
			we.external_id,
			we.url,
			we.signing_secret,
			we.status
		from updated_jobs u
		left join webhook_endpoints we
			on we.id = nullif(u.payload->>'webhook_endpoint_id', '')::bigint
			and we.deleted_at is null
	`, limit, workerID, leaseDuration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []WebhookDeliveryJob
	for rows.Next() {
		var job WebhookDeliveryJob
		var event []byte
		var endpointID sql.NullInt64
		var endpointExternalID, endpointURL, endpointSecret, endpointStatus sql.NullString
		if err := rows.Scan(
			&job.ID,
			&job.ExternalID,
			&job.WorkspaceID,
			&job.EventType,
			&event,
			&job.Attempts,
			&endpointID,
			&endpointExternalID,
			&endpointURL,
			&endpointSecret,
			&endpointStatus,
		); err != nil {
			return nil, err
		}
		job.Event = copyRaw(event)
		if endpointID.Valid {
			value := endpointID.Int64
			job.WebhookEndpointID = &value
		}
		if endpointExternalID.Valid {
			job.WebhookEndpointExternalID = endpointExternalID.String
		}
		if endpointURL.Valid {
			job.WebhookEndpointURL = endpointURL.String
		}
		if endpointSecret.Valid {
			job.WebhookEndpointSecret = endpointSecret.String
		}
		if endpointStatus.Valid {
			job.WebhookEndpointStatus = endpointStatus.String
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (d *DB) CompleteWebhookDeliveryJob(ctx context.Context, jobID int64) error {
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = 'completed',
			locked_by = null,
			locked_until = null,
			updated_at = now()
		where id = $1 and type = 'webhook_delivery'
	`, jobID)
	return err
}

func (d *DB) FailWebhookDeliveryJob(ctx context.Context, jobID int64, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	_, err := d.Pool.Exec(ctx, `
		update jobs
		set status = $2,
			locked_by = null,
			locked_until = null,
			run_after = $3,
			updated_at = now(),
			attempts = $5,
			payload = payload || jsonb_build_object('last_error', $4::text)
		where id = $1 and type = 'webhook_delivery'
	`, jobID, status, runAfter, reason, nextAttempts)
	return err
}
