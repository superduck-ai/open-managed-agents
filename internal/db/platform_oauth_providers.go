package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type PlatformOAuthProvider struct {
	UUID                       string
	ExternalID                 string
	Name                       string
	AuthorizeEndpoint          string
	TokenEndpoint              string
	Scope                      string
	ClientID                   string
	ClientSecretEnvelope       *secrets.Envelope
	TokenEndpointAuthMethod    string
	TokenEndpointEncoding      string
	CreatedByPlatformAdminUUID string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

func (d *DB) CreatePlatformOAuthProvider(ctx context.Context, provider PlatformOAuthProvider) (PlatformOAuthProvider, error) {
	params, err := platformOAuthProviderUpsertParams(provider)
	if err != nil {
		return PlatformOAuthProvider{}, err
	}
	mapper := NewPlatformOAuthProviderMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return PlatformOAuthProvider{}, ErrDuplicate
		}
		return PlatformOAuthProvider{}, err
	}
	return row.provider()
}

func (d *DB) ListPlatformOAuthProviders(ctx context.Context) ([]PlatformOAuthProvider, error) {
	mapper := NewPlatformOAuthProviderMapper(d.mapperDB)
	rows, err := mapper.List(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]PlatformOAuthProvider, 0, len(rows))
	for _, row := range rows {
		provider, err := row.provider()
		if err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func (d *DB) GetPlatformOAuthProviderByExternalID(ctx context.Context, externalID string) (PlatformOAuthProvider, error) {
	mapper := NewPlatformOAuthProviderMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, externalID)
	if err != nil {
		return PlatformOAuthProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) UpdatePlatformOAuthProvider(ctx context.Context, provider PlatformOAuthProvider) (PlatformOAuthProvider, error) {
	params, err := platformOAuthProviderUpsertParams(provider)
	if err != nil {
		return PlatformOAuthProvider{}, err
	}
	mapper := NewPlatformOAuthProviderMapper(d.mapperDB)
	row, err := mapper.UpdateByExternalID(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return PlatformOAuthProvider{}, ErrDuplicate
		}
		return PlatformOAuthProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) DeletePlatformOAuthProvider(ctx context.Context, externalID string) error {
	mapper := NewPlatformOAuthProviderMapper(d.mapperDB)
	rowsAffected, err := mapper.DeleteByExternalID(ctx, externalID)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func platformOAuthProviderUpsertParams(provider PlatformOAuthProvider) (upsertPlatformOAuthProviderParams, error) {
	if err := requireCompleteSecretEnvelope(provider.ClientSecretEnvelope); err != nil {
		return upsertPlatformOAuthProviderParams{}, err
	}
	encoded, err := json.Marshal(provider.ClientSecretEnvelope)
	if err != nil {
		return upsertPlatformOAuthProviderParams{}, fmt.Errorf("encode platform oauth provider secret: %w", err)
	}
	return upsertPlatformOAuthProviderParams{
		UUID:                       provider.UUID,
		ExternalID:                 provider.ExternalID,
		Name:                       provider.Name,
		AuthorizeEndpoint:          provider.AuthorizeEndpoint,
		TokenEndpoint:              provider.TokenEndpoint,
		Scope:                      provider.Scope,
		ClientID:                   provider.ClientID,
		ClientSecretEnvelope:       string(encoded),
		TokenEndpointAuthMethod:    provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding:      provider.TokenEndpointEncoding,
		CreatedByPlatformAdminUUID: provider.CreatedByPlatformAdminUUID,
		CreatedAt:                  provider.CreatedAt,
		UpdatedAt:                  provider.UpdatedAt,
	}, nil
}

func (r platformOAuthProviderRow) provider() (PlatformOAuthProvider, error) {
	provider := PlatformOAuthProvider{
		UUID:                       r.UUID,
		ExternalID:                 r.ExternalID,
		Name:                       r.Name,
		AuthorizeEndpoint:          r.AuthorizeEndpoint,
		TokenEndpoint:              r.TokenEndpoint,
		Scope:                      r.Scope,
		ClientID:                   r.ClientID,
		TokenEndpointAuthMethod:    r.TokenEndpointAuthMethod,
		TokenEndpointEncoding:      r.TokenEndpointEncoding,
		CreatedByPlatformAdminUUID: r.CreatedByPlatformAdminUUID,
		CreatedAt:                  r.CreatedAt,
		UpdatedAt:                  r.UpdatedAt,
	}
	var envelope secrets.Envelope
	if err := json.Unmarshal([]byte(r.ClientSecretEnvelope), &envelope); err != nil {
		return PlatformOAuthProvider{}, fmt.Errorf("decode platform oauth provider secret: %w", err)
	}
	provider.ClientSecretEnvelope = &envelope
	return provider, nil
}
