package db

import (
	"context"
	"encoding/json"
	"time"
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
	FailureStartedAt    *time.Time
	ConsecutiveFailures int
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

func (d *DB) GetWorkspaceIdentifiers(ctx context.Context, workspaceUUID string) (WorkspaceIdentifiers, error) {
	mapper := NewWebhookWorkspaceMapper(d.mapperDB)
	row, err := mapper.FindIdentifiers(ctx, workspaceUUID)
	if err != nil {
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
	mapper := NewWebhookEndpointMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, insertWebhookEndpointParams{
		UUID:                endpoint.UUID,
		ExternalID:          endpoint.ExternalID,
		OrganizationUUID:    endpoint.OrganizationUUID,
		WorkspaceUUID:       endpoint.WorkspaceUUID,
		CreatedByAPIKeyUUID: nullableString(endpoint.CreatedByAPIKeyUUID),
		URL:                 endpoint.URL,
		Name:                endpoint.Name,
		Description:         endpoint.Description,
		EnabledEvents:       events,
		SigningSecret:       endpoint.SigningSecret,
		Status:              endpoint.Status,
		DisabledReason:      endpoint.DisabledReason,
		ConsecutiveFailures: endpoint.ConsecutiveFailures,
		CreatedAt:           endpoint.CreatedAt,
	})
	if err != nil {
		return WebhookEndpoint{}, mapNoRows(err)
	}
	return row.endpoint()
}

func (d *DB) ListWebhookEndpoints(ctx context.Context, workspaceUUID string) ([]WebhookEndpoint, error) {
	mapper := NewWebhookEndpointMapper(d.mapperDB)
	rows, err := mapper.List(ctx, workspaceUUID)
	if err != nil {
		return nil, err
	}
	return webhookEndpoints(rows)
}

func (d *DB) GetWebhookEndpoint(ctx context.Context, workspaceUUID string, externalID string) (WebhookEndpoint, error) {
	mapper := NewWebhookEndpointMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return WebhookEndpoint{}, mapNoRows(err)
	}
	return row.endpoint()
}

// WebhookEndpointUpdate changes only explicitly provided subscription fields.
type WebhookEndpointUpdate struct {
	URL           *string
	Name          *string
	Description   *string
	EnabledEvents []string
	Status        *string
	UpdatedAt     time.Time
}

func (d *DB) UpdateWebhookEndpoint(ctx context.Context, workspaceUUID string, externalID string, next WebhookEndpointUpdate) (WebhookEndpoint, error) {
	var events []byte
	if next.EnabledEvents != nil {
		var err error
		events, err = json.Marshal(next.EnabledEvents)
		if err != nil {
			return WebhookEndpoint{}, err
		}
	}
	mapper := NewWebhookEndpointMapper(d.mapperDB)
	row, err := mapper.UpdateByExternalID(ctx, updateWebhookEndpointParams{
		WorkspaceUUID: workspaceUUID, ExternalID: externalID,
		URL: next.URL, Name: next.Name, Description: next.Description,
		EnabledEvents: events, Status: next.Status, UpdatedAt: next.UpdatedAt,
	})
	if err != nil {
		return WebhookEndpoint{}, mapNoRows(err)
	}
	return row.endpoint()
}

func (d *DB) RegenerateWebhookEndpointSigningSecret(ctx context.Context, workspaceUUID string, externalID string, signingSecret string, updatedAt time.Time) error {
	mapper := NewWebhookEndpointMapper(d.mapperDB)
	rowsAffected, err := mapper.UpdateSigningSecret(ctx, regenerateWebhookEndpointSecretParams{
		WorkspaceUUID: workspaceUUID,
		ExternalID:    externalID,
		SigningSecret: signingSecret,
		UpdatedAt:     updatedAt,
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
	mapper := NewWebhookEndpointMapper(d.mapperDB)
	rowsAffected, err := mapper.SoftDeleteByExternalID(ctx, workspaceUUID, externalID)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (d *DB) ListActiveWebhookEndpointUUIDs(ctx context.Context, workspaceUUID, eventType string) ([]string, error) {
	rows, err := NewWebhookEndpointMapper(d.mapperDB).ListActiveForEvent(ctx, workspaceUUID, eventType)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.UUID
	}
	return ids, nil
}

func webhookEndpoints(rows []webhookEndpointRow) ([]WebhookEndpoint, error) {
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
		CreatedByAPIKeyUUID: stringFromNullable(r.CreatedByAPIKeyUUID),
		URL:                 r.URL,
		Name:                r.Name,
		Description:         r.Description,
		EnabledEvents:       events,
		SigningSecret:       r.SigningSecret,
		Status:              r.Status,
		ConsecutiveFailures: r.ConsecutiveFailures,
		FailureStartedAt:    r.FailureStartedAt,
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
