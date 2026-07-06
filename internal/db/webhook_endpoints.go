package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type WorkspaceIdentifiers struct {
	OrganizationExternalID string
	WorkspaceExternalID    string
}

type WebhookEndpoint struct {
	ID                  int64
	UUID                string
	ExternalID          string
	OrganizationID      int64
	WorkspaceID         int64
	CreatedByAPIKeyID   int64
	URL                 string
	Name                string
	Description         string
	EnabledEvents       []string
	SigningSecret       string
	Status              string
	DisabledReason      *string
	ConsecutiveFailures int
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

func (d *DB) GetWorkspaceIdentifiers(ctx context.Context, workspaceID int64) (WorkspaceIdentifiers, error) {
	var ids WorkspaceIdentifiers
	err := d.Pool.QueryRow(ctx, `
		select o.external_id, w.external_id
		from workspaces w
		join organizations o on o.id = w.organization_id
		where w.id = $1
	`, workspaceID).Scan(&ids.OrganizationExternalID, &ids.WorkspaceExternalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkspaceIdentifiers{}, ErrNotFound
	}
	return ids, err
}

func (d *DB) CreateWebhookEndpoint(ctx context.Context, endpoint WebhookEndpoint) (WebhookEndpoint, error) {
	events, err := json.Marshal(endpoint.EnabledEvents)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	return scanWebhookEndpoint(d.Pool.QueryRow(ctx, `
		insert into webhook_endpoints (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			url, name, description, enabled_events, signing_secret, status,
			disabled_reason, consecutive_failures, created_at, updated_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9::jsonb, $10, $11,
			$12, $13, $14, $14
		)
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, url, name, description, enabled_events,
			signing_secret, status, disabled_reason, consecutive_failures,
			created_at, updated_at, deleted_at
	`, endpoint.UUID, endpoint.ExternalID, endpoint.OrganizationID, endpoint.WorkspaceID,
		endpoint.CreatedByAPIKeyID, endpoint.URL, endpoint.Name, endpoint.Description,
		jsonArg(json.RawMessage(events)), endpoint.SigningSecret, endpoint.Status, endpoint.DisabledReason,
		endpoint.ConsecutiveFailures, endpoint.CreatedAt))
}

func (d *DB) ListWebhookEndpoints(ctx context.Context, workspaceID int64) ([]WebhookEndpoint, error) {
	rows, err := d.Pool.Query(ctx, webhookEndpointSelectSQL()+`
		where workspace_id = $1 and deleted_at is null
		order by created_at desc, id desc
	`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWebhookEndpointRows(rows)
}

func (d *DB) GetWebhookEndpoint(ctx context.Context, workspaceID int64, externalID string) (WebhookEndpoint, error) {
	return scanWebhookEndpoint(d.Pool.QueryRow(ctx, webhookEndpointSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) UpdateWebhookEndpoint(ctx context.Context, workspaceID int64, externalID string, next WebhookEndpoint) (WebhookEndpoint, error) {
	events, err := json.Marshal(next.EnabledEvents)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	return scanWebhookEndpoint(d.Pool.QueryRow(ctx, `
		update webhook_endpoints
		set url = $3,
			name = $4,
			description = $5,
			enabled_events = $6::jsonb,
			status = $7,
			disabled_reason = $8,
			consecutive_failures = $9,
			updated_at = $10
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, url, name, description, enabled_events,
			signing_secret, status, disabled_reason, consecutive_failures,
			created_at, updated_at, deleted_at
	`, workspaceID, externalID, next.URL, next.Name, next.Description, jsonArg(json.RawMessage(events)),
		next.Status, next.DisabledReason, next.ConsecutiveFailures, next.UpdatedAt))
}

func (d *DB) RegenerateWebhookEndpointSigningSecret(ctx context.Context, workspaceID int64, externalID string, signingSecret string, updatedAt time.Time) error {
	tag, err := d.Pool.Exec(ctx, `
		update webhook_endpoints
		set signing_secret = $3,
			updated_at = $4
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID, signingSecret, updatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeleteWebhookEndpoint(ctx context.Context, workspaceID int64, externalID string) error {
	tag, err := d.Pool.Exec(ctx, `
		update webhook_endpoints
		set deleted_at = now(),
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) HasWebhookEndpoints(ctx context.Context, workspaceID int64) (bool, error) {
	var exists bool
	err := d.Pool.QueryRow(ctx, `
		select exists(
			select 1
			from webhook_endpoints
			where workspace_id = $1 and deleted_at is null
		)
	`, workspaceID).Scan(&exists)
	return exists, err
}

func (d *DB) ListActiveWebhookEndpointsForEvent(ctx context.Context, workspaceID int64, eventType string) ([]WebhookEndpoint, error) {
	rows, err := d.Pool.Query(ctx, webhookEndpointSelectSQL()+`
		where workspace_id = $1
			and deleted_at is null
			and status = 'enabled'
			and enabled_events ? $2
		order by created_at asc, id asc
	`, workspaceID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWebhookEndpointRows(rows)
}

func (d *DB) RecordWebhookEndpointDeliverySuccess(ctx context.Context, endpointID int64) error {
	_, err := d.Pool.Exec(ctx, `
		update webhook_endpoints
		set consecutive_failures = 0,
			disabled_reason = null,
			updated_at = now()
		where id = $1 and deleted_at is null and status = 'enabled'
	`, endpointID)
	return err
}

func (d *DB) RecordWebhookEndpointDeliveryFailure(ctx context.Context, endpointID int64, reason string, disableAfter int) error {
	if disableAfter <= 0 {
		disableAfter = 20
	}
	_, err := d.Pool.Exec(ctx, `
		update webhook_endpoints
		set consecutive_failures = consecutive_failures + 1,
			status = case when consecutive_failures + 1 >= $2 then 'disabled' else status end,
			disabled_reason = case when consecutive_failures + 1 >= $2 then $3 else disabled_reason end,
			updated_at = now()
		where id = $1 and deleted_at is null and status = 'enabled'
	`, endpointID, disableAfter, truncateWebhookFailureReason(reason))
	return err
}

func webhookEndpointSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, url, name, description, enabled_events,
			signing_secret, status, disabled_reason, consecutive_failures,
			created_at, updated_at, deleted_at
		from webhook_endpoints
	`
}

type webhookEndpointScanner interface {
	Scan(dest ...any) error
}

type webhookEndpointRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanWebhookEndpoint(row webhookEndpointScanner) (WebhookEndpoint, error) {
	var endpoint WebhookEndpoint
	var events []byte
	err := row.Scan(&endpoint.ID, &endpoint.UUID, &endpoint.ExternalID,
		&endpoint.OrganizationID, &endpoint.WorkspaceID, &endpoint.CreatedByAPIKeyID,
		&endpoint.URL, &endpoint.Name, &endpoint.Description, &events,
		&endpoint.SigningSecret, &endpoint.Status, &endpoint.DisabledReason,
		&endpoint.ConsecutiveFailures, &endpoint.CreatedAt, &endpoint.UpdatedAt,
		&endpoint.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return WebhookEndpoint{}, ErrNotFound
	}
	if err != nil {
		return WebhookEndpoint{}, err
	}
	if len(events) > 0 {
		if err := json.Unmarshal(events, &endpoint.EnabledEvents); err != nil {
			return WebhookEndpoint{}, err
		}
	}
	return endpoint, nil
}

func scanWebhookEndpointRows(rows webhookEndpointRows) ([]WebhookEndpoint, error) {
	var endpoints []WebhookEndpoint
	for rows.Next() {
		endpoint, err := scanWebhookEndpoint(rows)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, rows.Err()
}

func truncateWebhookFailureReason(reason string) string {
	if len(reason) <= 1000 {
		return reason
	}
	return reason[:1000]
}
