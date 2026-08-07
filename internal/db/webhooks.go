package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
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

// WebhookDeliveryEvent is a prepared event that can join a caller-owned transaction.
type WebhookDeliveryEvent struct {
	EventType       string
	Event           json.RawMessage
	FallbackEnabled bool
}

type webhookDeliveryJobPayload struct {
	EventType           string          `json:"event_type"`
	Event               json.RawMessage `json:"event"`
	WebhookEndpointUUID string          `json:"webhook_endpoint_uuid,omitempty"`
}

func (d *DB) EnqueueWebhookDeliveryJob(ctx context.Context, workspaceUUID, eventType string, event json.RawMessage) error {
	payload, err := webhookDeliveryJobPayloadJSON(eventType, event, "")
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
	payload, err := webhookDeliveryJobPayloadJSON(eventType, event, parsedEndpointUUID.String())
	if err != nil {
		return err
	}
	mapper := NewWebhookDeliveryJobMapper(d.mapperDB)
	return mapper.Insert(ctx, workspaceUUID, payload)
}

func enqueueWebhookDeliveryEventsTx(ctx context.Context, executor yourbatis.Executor, workspaceUUID string, events []WebhookDeliveryEvent) error {
	if len(events) == 0 {
		return nil
	}
	endpointMapper := NewWebhookEndpointMapper(executor)
	hasEndpoints, err := endpointMapper.Exists(ctx, workspaceUUID)
	if err != nil {
		return err
	}
	jobMapper := NewWebhookDeliveryJobMapper(executor)
	for _, event := range events {
		if !hasEndpoints {
			if !event.FallbackEnabled {
				continue
			}
			payload, err := webhookDeliveryJobPayloadJSON(event.EventType, event.Event, "")
			if err != nil {
				return err
			}
			if err := jobMapper.Insert(ctx, workspaceUUID, payload); err != nil {
				return err
			}
			continue
		}

		endpoints, err := endpointMapper.ListActiveForEvent(ctx, workspaceUUID, event.EventType)
		if err != nil {
			return err
		}
		for _, endpoint := range endpoints {
			payload, err := webhookDeliveryJobPayloadJSON(event.EventType, event.Event, endpoint.UUID)
			if err != nil {
				return err
			}
			if err := jobMapper.Insert(ctx, workspaceUUID, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

func webhookDeliveryJobPayloadJSON(eventType string, event json.RawMessage, endpointUUID string) ([]byte, error) {
	return json.Marshal(webhookDeliveryJobPayload{
		EventType:           eventType,
		Event:               event,
		WebhookEndpointUUID: endpointUUID,
	})
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
		Event:                     copyRaw(r.Event),
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
