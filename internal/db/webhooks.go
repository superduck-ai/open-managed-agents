package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// WebhookDeliveryTarget contains only the current configuration needed to send.
type WebhookDeliveryTarget struct {
	URL           string
	SigningSecret string
	Status        string
}

func (d *DB) FindWebhookDeliveryTarget(ctx context.Context, workspaceUUID, endpointUUID string) (WebhookDeliveryTarget, bool, error) {
	row, found, err := NewWebhookEndpointMapper(d.mapperDB).FindDeliveryTarget(ctx, workspaceUUID, endpointUUID)
	return WebhookDeliveryTarget{URL: row.URL, SigningSecret: row.SigningSecret, Status: row.Status}, found, err
}
func (d *DB) RecordWebhookDeliverySuccess(ctx context.Context, workspaceUUID, endpointUUID string) error {
	return NewWebhookEndpointMapper(d.mapperDB).RecordDeliverySuccess(ctx, endpointUUID, workspaceUUID)
}

// RecordWebhookDeliveryFailure returns whether this observation disabled the endpoint.
// Message acknowledgment is deliberately outside this database operation.
func (d *DB) RecordWebhookDeliveryFailure(ctx context.Context, workspaceUUID, endpointUUID, reason string, immediate bool, disableAfter time.Duration) (bool, error) {
	row, err := NewWebhookEndpointMapper(d.mapperDB).RecordDeliveryFailure(ctx, recordWebhookEndpointFailureParams{
		WorkspaceUUID: workspaceUUID, EndpointUUID: endpointUUID, Reason: reason, ImmediateDisable: immediate, DisableAfterMicroseconds: disableAfter.Microseconds(),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return row.Disabled, err
}
