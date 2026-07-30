package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const (
	webhookEndpointColumns = `
		CAST(uuid AS text) AS uuid,
		external_id,
		CAST(organization_uuid AS text) AS organization_uuid,
		CAST(workspace_uuid AS text) AS workspace_uuid,
		CAST(created_by_api_key_uuid AS text) AS created_by_api_key_uuid,
		url,
		name,
		description,
		enabled_events,
		signing_secret,
		status,
		disabled_reason,
		consecutive_failures,
		created_at,
		updated_at,
		deleted_at
	`
	getWorkspaceIdentifiersQuery = `
		select
			CAST(w.organization_uuid AS text) AS organization_uuid,
			w.external_id AS workspace_external_id
		from workspaces w
		where w.uuid = CAST(:workspace_uuid AS uuid)
	`
	createWebhookEndpointQuery = `
		insert into webhook_endpoints (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			url, name, description, enabled_events, signing_secret, status,
			disabled_reason, consecutive_failures, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :created_by_api_key_uuid,
			:url, :name, :description, CAST(:enabled_events AS jsonb), :signing_secret, :status,
			:disabled_reason, :consecutive_failures, :created_at, :created_at
		)
		returning ` + webhookEndpointColumns + `
	`
	listWebhookEndpointsQuery = `
		select ` + webhookEndpointColumns + `
		from webhook_endpoints
		where workspace_uuid = :workspace_uuid and deleted_at is null
		order by created_at desc, uuid desc
	`
	getWebhookEndpointQuery = `
		select ` + webhookEndpointColumns + `
		from webhook_endpoints
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
	`
	updateWebhookEndpointQuery = `
		update webhook_endpoints
		set url = :url,
			name = :name,
			description = :description,
			enabled_events = CAST(:enabled_events AS jsonb),
			status = :status,
			disabled_reason = :disabled_reason,
			consecutive_failures = :consecutive_failures,
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
		returning ` + webhookEndpointColumns + `
	`
	regenerateWebhookEndpointSigningSecretQuery = `
		update webhook_endpoints
		set signing_secret = :signing_secret,
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
	`
	deleteWebhookEndpointQuery = `
		update webhook_endpoints
		set deleted_at = now(),
			updated_at = now()
		where workspace_uuid = :workspace_uuid
			and external_id = :external_id
			and deleted_at is null
	`
	hasWebhookEndpointsQuery = `
		select exists(
			select 1
			from webhook_endpoints
			where workspace_uuid = :workspace_uuid and deleted_at is null
		)
	`
	listActiveWebhookEndpointsForEventQuery = `
		select ` + webhookEndpointColumns + `
		from webhook_endpoints
		where workspace_uuid = :workspace_uuid
			and deleted_at is null
			and status = 'enabled'
			and jsonb_exists(enabled_events, :event_type)
		order by created_at asc, uuid asc
	`
	recordWebhookEndpointDeliverySuccessQuery = `
		update webhook_endpoints
		set consecutive_failures = 0,
			disabled_reason = null,
			updated_at = now()
		where uuid = CAST(:endpoint_uuid AS uuid) and deleted_at is null and status = 'enabled'
	`
	recordWebhookEndpointDeliveryFailureQuery = `
		update webhook_endpoints
		set consecutive_failures = consecutive_failures + 1,
			status = case
				when consecutive_failures + 1 >= :disable_after then 'disabled'
				else status
			end,
			disabled_reason = case
				when consecutive_failures + 1 >= :disable_after then :reason
				else disabled_reason
			end,
			updated_at = now()
		where uuid = CAST(:endpoint_uuid AS uuid) and deleted_at is null and status = 'enabled'
	`
)

type WorkspaceIdentifiers struct {
	OrganizationUUID    string
	WorkspaceExternalID string
}

type WebhookEndpoint struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
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

type workspaceIdentifiersRow struct {
	OrganizationUUID    string `db:"organization_uuid"`
	WorkspaceExternalID string `db:"workspace_external_id"`
}

type webhookEndpointRow struct {
	UUID                string         `db:"uuid"`
	ExternalID          string         `db:"external_id"`
	OrganizationUUID    string         `db:"organization_uuid"`
	WorkspaceUUID       string         `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string         `db:"created_by_api_key_uuid"`
	URL                 string         `db:"url"`
	Name                string         `db:"name"`
	Description         string         `db:"description"`
	EnabledEvents       []byte         `db:"enabled_events"`
	SigningSecret       string         `db:"signing_secret"`
	Status              string         `db:"status"`
	DisabledReason      sql.NullString `db:"disabled_reason"`
	ConsecutiveFailures int            `db:"consecutive_failures"`
	CreatedAt           time.Time      `db:"created_at"`
	UpdatedAt           time.Time      `db:"updated_at"`
	DeletedAt           *time.Time     `db:"deleted_at"`
}

func (d *DB) GetWorkspaceIdentifiers(ctx context.Context, workspaceUUID string) (WorkspaceIdentifiers, error) {
	var row workspaceIdentifiersRow
	if err := namedGetContext(ctx, d.sql, &row, getWorkspaceIdentifiersQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
	}); err != nil {
		return WorkspaceIdentifiers{}, mapNoRows(err)
	}
	return WorkspaceIdentifiers{
		OrganizationUUID:    row.OrganizationUUID,
		WorkspaceExternalID: row.WorkspaceExternalID,
	}, nil
}

func (d *DB) CreateWebhookEndpoint(ctx context.Context, endpoint WebhookEndpoint) (WebhookEndpoint, error) {
	events, err := json.Marshal(endpoint.EnabledEvents)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	return getWebhookEndpointSQLX(ctx, d.sql, createWebhookEndpointQuery, map[string]any{
		"uuid":                    endpoint.UUID,
		"external_id":             endpoint.ExternalID,
		"organization_uuid":       endpoint.OrganizationUUID,
		"workspace_uuid":          endpoint.WorkspaceUUID,
		"created_by_api_key_uuid": endpoint.CreatedByAPIKeyUUID,
		"url":                     endpoint.URL,
		"name":                    endpoint.Name,
		"description":             endpoint.Description,
		"enabled_events":          jsonArg(json.RawMessage(events)),
		"signing_secret":          endpoint.SigningSecret,
		"status":                  endpoint.Status,
		"disabled_reason":         endpoint.DisabledReason,
		"consecutive_failures":    endpoint.ConsecutiveFailures,
		"created_at":              endpoint.CreatedAt,
	})
}

func (d *DB) ListWebhookEndpoints(ctx context.Context, workspaceUUID string) ([]WebhookEndpoint, error) {
	return selectWebhookEndpointsSQLX(ctx, d.sql, listWebhookEndpointsQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
	})
}

func (d *DB) GetWebhookEndpoint(ctx context.Context, workspaceUUID string, externalID string) (WebhookEndpoint, error) {
	return getWebhookEndpointSQLX(ctx, d.sql, getWebhookEndpointQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	})
}

func (d *DB) UpdateWebhookEndpoint(ctx context.Context, workspaceUUID string, externalID string, next WebhookEndpoint) (WebhookEndpoint, error) {
	events, err := json.Marshal(next.EnabledEvents)
	if err != nil {
		return WebhookEndpoint{}, err
	}
	return getWebhookEndpointSQLX(ctx, d.sql, updateWebhookEndpointQuery, map[string]any{
		"workspace_uuid":       workspaceUUID,
		"external_id":          externalID,
		"url":                  next.URL,
		"name":                 next.Name,
		"description":          next.Description,
		"enabled_events":       jsonArg(json.RawMessage(events)),
		"status":               next.Status,
		"disabled_reason":      next.DisabledReason,
		"consecutive_failures": next.ConsecutiveFailures,
		"updated_at":           next.UpdatedAt,
	})
}

func (d *DB) RegenerateWebhookEndpointSigningSecret(ctx context.Context, workspaceUUID string, externalID string, signingSecret string, updatedAt time.Time) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, regenerateWebhookEndpointSigningSecretQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
		"signing_secret": signingSecret,
		"updated_at":     updatedAt,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) DeleteWebhookEndpoint(ctx context.Context, workspaceUUID string, externalID string) error {
	rowsAffected, err := namedExecRowsAffected(ctx, d.sql, deleteWebhookEndpointQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
		"external_id":    externalID,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) HasWebhookEndpoints(ctx context.Context, workspaceUUID string) (bool, error) {
	var exists bool
	err := namedGetContext(ctx, d.sql, &exists, hasWebhookEndpointsQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
	})
	return exists, err
}

func (d *DB) ListActiveWebhookEndpointsForEvent(ctx context.Context, workspaceUUID string, eventType string) ([]WebhookEndpoint, error) {
	return selectWebhookEndpointsSQLX(ctx, d.sql, listActiveWebhookEndpointsForEventQuery, map[string]any{
		"workspace_uuid": workspaceUUID,
		"event_type":     eventType,
	})
}

func (d *DB) RecordWebhookEndpointDeliverySuccess(ctx context.Context, endpointUUID string) error {
	_, err := namedExecContext(ctx, d.sql, recordWebhookEndpointDeliverySuccessQuery, map[string]any{
		"endpoint_uuid": endpointUUID,
	})
	return err
}

func (d *DB) RecordWebhookEndpointDeliveryFailure(ctx context.Context, endpointUUID string, reason string, disableAfter int) error {
	if disableAfter <= 0 {
		disableAfter = 20
	}
	_, err := namedExecContext(ctx, d.sql, recordWebhookEndpointDeliveryFailureQuery, map[string]any{
		"endpoint_uuid": endpointUUID,
		"disable_after": disableAfter,
		"reason":        truncateWebhookFailureReason(reason),
	})
	return err
}

func getWebhookEndpointSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) (WebhookEndpoint, error) {
	var row webhookEndpointRow
	if err := namedGetContext(ctx, database, &row, query, arguments); err != nil {
		return WebhookEndpoint{}, mapNoRows(err)
	}
	return row.endpoint()
}

func selectWebhookEndpointsSQLX(
	ctx context.Context,
	database sqlxNamedQueryer,
	query string,
	arguments map[string]any,
) ([]WebhookEndpoint, error) {
	var rows []webhookEndpointRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	endpoints := make([]WebhookEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoint, err := row.endpoint()
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func (r webhookEndpointRow) endpoint() (WebhookEndpoint, error) {
	var events []string
	if len(r.EnabledEvents) > 0 {
		if err := json.Unmarshal(r.EnabledEvents, &events); err != nil {
			return WebhookEndpoint{}, err
		}
	}
	endpoint := WebhookEndpoint{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		URL:                 r.URL,
		Name:                r.Name,
		Description:         r.Description,
		EnabledEvents:       events,
		SigningSecret:       r.SigningSecret,
		Status:              r.Status,
		ConsecutiveFailures: r.ConsecutiveFailures,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		DeletedAt:           r.DeletedAt,
	}
	if r.DisabledReason.Valid {
		disabledReason := r.DisabledReason.String
		endpoint.DisabledReason = &disabledReason
	}
	return endpoint, nil
}

func truncateWebhookFailureReason(reason string) string {
	if len(reason) <= 1000 {
		return reason
	}
	return reason[:1000]
}
