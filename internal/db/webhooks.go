package db

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
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
	rows, err := mapper.Lease(ctx, workerID, limit, leaseDuration.Microseconds())
	if err != nil {
		return nil, err
	}

	jobs := make([]WebhookDeliveryJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, row.job())
	}
	return jobs, nil
}

func (d *DB) CompleteWebhookDeliveryJob(ctx context.Context, jobUUID string) error {
	mapper := NewWebhookDeliveryJobMapper(d.mapperDB)
	return mapper.Complete(ctx, jobUUID)
}

func (d *DB) FailWebhookDeliveryJob(ctx context.Context, jobUUID string, attempts int, reason string, retryDelay time.Duration, maxAttempts int) error {
	nextAttempts := attempts + 1
	status := "retry"
	if nextAttempts >= maxAttempts {
		status = "failed"
	}
	runAfter := time.Now().UTC().Add(retryDelay)
	mapper := NewWebhookDeliveryJobMapper(d.mapperDB)
	return mapper.Fail(ctx, failWebhookDeliveryJobParams{
		JobUUID:  jobUUID,
		Status:   status,
		RunAfter: runAfter,
		Attempts: nextAttempts,
		Reason:   reason,
	})
}

func (r webhookDeliveryJobRow) job() WebhookDeliveryJob {
	job := WebhookDeliveryJob{
		UUID:                      r.UUID,
		ExternalID:                r.ExternalID,
		WorkspaceUUID:             r.WorkspaceUUID,
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
