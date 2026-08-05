package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper WebhookDeliveryJobMapper -sql ./webhook_delivery_job_mapper.xml -out ./webhook_delivery_job_mapper.sqlmap.gen.go -dialect postgres

type webhookDeliveryJobRow struct {
	UUID                      string         `db:"uuid"`
	ExternalID                string         `db:"external_id"`
	WorkspaceUUID             string         `db:"workspace_uuid"`
	EventType                 string         `db:"event_type"`
	Event                     []byte         `db:"event"`
	Attempts                  int            `db:"attempts"`
	WebhookEndpointUUID       sql.NullString `db:"webhook_endpoint_uuid"`
	WebhookEndpointExternalID sql.NullString `db:"webhook_endpoint_external_id"`
	WebhookEndpointURL        sql.NullString `db:"webhook_endpoint_url"`
	WebhookEndpointSecret     sql.NullString `db:"webhook_endpoint_secret"`
	WebhookEndpointStatus     sql.NullString `db:"webhook_endpoint_status"`
}

type failWebhookDeliveryJobParams struct {
	JobUUID  string
	Status   string
	RunAfter time.Time
	Attempts int
	Reason   string
}

type WebhookDeliveryJobMapper interface {
	Insert(ctx context.Context, workspaceUUID string, payload []byte) error
	Lease(ctx context.Context, workerID string, limit int, leaseMicroseconds int64) ([]webhookDeliveryJobRow, error)
	Complete(ctx context.Context, jobUUID string) error
	Fail(ctx context.Context, params failWebhookDeliveryJobParams) error
}
