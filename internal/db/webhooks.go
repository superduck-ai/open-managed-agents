package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"uuid"

	"github.com/superduck-ai/yourbatis"
)

type WebhookDeliveryJob struct {
	UUID                      string
	ExternalID                string
	WorkspaceUUID             string
	ClaimToken                string
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

func (d *DB) EnqueueWebhookDeliveryJob(ctx context.Context, workspaceUUID, eventType string, event json.RawMessage) error {
	payload, err := json.Marshal(webhookDeliveryJobPayload{EventType: eventType, Event: event})
	if err != nil {
		return err
	}
	mapper := NewWebhookDeliveryJobMapper(d.mapperDB)
	return mapper.Insert(ctx, workspaceUUID, payload)
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
	mapper := NewWebhookDeliveryJobMapper(d.mapperDB)
	return mapper.Insert(ctx, workspaceUUID, payload)
}

func (d *DB) LeaseWebhookDeliveryJobs(ctx context.Context, workerID string, limit int, leaseDuration time.Duration) ([]WebhookDeliveryJob, error) {
	if limit <= 0 {
		limit = 10
	}
	if leaseDuration <= 0 {
		leaseDuration = time.Minute
	}
	mapper := NewWebhookDeliveryJobMapper(d.mapperDB)
	rows, err := mapper.Lease(ctx, workerID+":"+uuid.NewV4().String(), limit, leaseDuration.Microseconds())
	if err != nil {
		return nil, err
	}

	jobs := make([]WebhookDeliveryJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

// CompleteWebhookDeliveryJob marks a current claim complete. Skips do not count as deliveries.
func (d *DB) CompleteWebhookDeliveryJob(ctx context.Context, job WebhookDeliveryJob, delivered bool) (bool, error) {
	var record func(WebhookEndpointMapper) error
	if delivered && job.WebhookEndpointUUID != nil {
		record = func(mapper WebhookEndpointMapper) error {
			return mapper.RecordDeliverySuccess(ctx, *job.WebhookEndpointUUID, job.WorkspaceUUID)
		}
	}
	return d.finishWebhookDeliveryJob(ctx, job, func(mapper WebhookDeliveryJobMapper) (int64, error) {
		return mapper.Complete(ctx, job.UUID, job.WorkspaceUUID, job.ClaimToken)
	}, record)
}

// WebhookDeliveryFailure separates terminal rejections from retryable failures.
type WebhookDeliveryFailure struct {
	Reason       string
	RetryDelay   time.Duration
	MaxAttempts  int
	DisableAfter time.Duration
	Terminal     bool
}

var errWebhookClaimExpired = errors.New("webhook claim expired during result transaction")

func (d *DB) FailWebhookDeliveryJob(ctx context.Context, job WebhookDeliveryJob, failure WebhookDeliveryFailure) (bool, error) {
	if failure.DisableAfter <= 0 {
		failure.DisableAfter = 24 * time.Hour
	}
	var applied bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewWebhookDeliveryJobMapper(executor)
		_, found, err := mapper.LockClaim(ctx, job.UUID, job.WorkspaceUUID, job.ClaimToken)
		if err != nil || !found {
			return err
		}
		disabled, err := recordWebhookFailure(ctx, executor, job, failure)
		if err != nil {
			return err
		}
		status := "retry"
		if failure.Terminal || disabled || job.Attempts+1 >= failure.MaxAttempts {
			status = "failed"
		}
		rows, err := mapper.Fail(ctx, failWebhookDeliveryJobParams{
			JobUUID: job.UUID, WorkspaceUUID: job.WorkspaceUUID, ClaimToken: job.ClaimToken,
			Status: status, RunAfter: time.Now().UTC().Add(failure.RetryDelay), Attempts: job.Attempts + 1, Reason: failure.Reason,
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			return errWebhookClaimExpired
		}
		applied = true
		return nil
	})
	if errors.Is(err, errWebhookClaimExpired) {
		return false, nil
	}
	return applied && err == nil, err
}

func recordWebhookFailure(ctx context.Context, executor yourbatis.Executor, job WebhookDeliveryJob, failure WebhookDeliveryFailure) (bool, error) {
	if job.WebhookEndpointUUID == nil {
		return false, nil
	}
	row, err := NewWebhookEndpointMapper(executor).RecordDeliveryFailure(ctx, recordWebhookEndpointFailureParams{
		EndpointUUID: *job.WebhookEndpointUUID, WorkspaceUUID: job.WorkspaceUUID,
		DisableAfterMicroseconds: failure.DisableAfter.Microseconds(), ImmediateDisable: failure.Terminal,
		Reason: truncateWebhookFailureReason(failure.Reason),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return row.Disabled, err
}

// ExhaustWebhookDeliveryJob stops an already exhausted claim without a new
// delivery attempt, preserving its payload, counters and scheduled time.
func (d *DB) ExhaustWebhookDeliveryJob(ctx context.Context, job WebhookDeliveryJob) (bool, error) {
	return d.finishWebhookDeliveryJob(ctx, job, func(mapper WebhookDeliveryJobMapper) (int64, error) {
		return mapper.Exhaust(ctx, job.UUID, job.WorkspaceUUID, job.ClaimToken)
	}, nil)
}

// The job row lock fences both the result and endpoint statistics until commit.
func (d *DB) finishWebhookDeliveryJob(ctx context.Context, job WebhookDeliveryJob, update func(WebhookDeliveryJobMapper) (int64, error), record func(WebhookEndpointMapper) error) (bool, error) {
	var applied bool
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewWebhookDeliveryJobMapper(executor)
		if record != nil {
			_, found, err := mapper.LockClaim(ctx, job.UUID, job.WorkspaceUUID, job.ClaimToken)
			if err != nil || !found {
				return err
			}
			if err := record(NewWebhookEndpointMapper(executor)); err != nil {
				return err
			}
		}
		rows, err := update(mapper)
		if err != nil {
			return err
		}
		if rows == 0 {
			return errWebhookClaimExpired
		}
		applied = true
		return nil
	})
	if errors.Is(err, errWebhookClaimExpired) {
		return false, nil
	}
	return applied && err == nil, err
}

func (r webhookDeliveryJobRow) job() WebhookDeliveryJob {
	job := WebhookDeliveryJob{
		UUID:                      r.UUID,
		ExternalID:                r.ExternalID,
		WorkspaceUUID:             r.WorkspaceUUID,
		ClaimToken:                r.ClaimToken,
		EventType:                 r.EventType,
		Event:                     bytes.Clone(r.Event),
		Attempts:                  r.Attempts,
		WebhookEndpointExternalID: r.WebhookEndpointExternalID.String,
		WebhookEndpointURL:        r.WebhookEndpointURL.String,
		WebhookEndpointSecret:     r.WebhookEndpointSecret.String,
		WebhookEndpointStatus:     r.WebhookEndpointStatus.String,
	}
	if r.WebhookEndpointUUID.Valid {
		endpointUUID := r.WebhookEndpointUUID.String
		job.WebhookEndpointUUID = &endpointUUID
	}
	return job
}
