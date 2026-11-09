package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	BuiltinKey                 string
	Protected                  bool
	Version                    int64
	DefaultSecretName          string
	DefaultAllowedHosts        []string
	ClientIDEnvelope           *secrets.Envelope
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
	row, err := mapper.UpdateByUUID(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return PlatformOAuthProvider{}, ErrDuplicate
		}
		if errors.Is(err, sql.ErrNoRows) {
			return PlatformOAuthProvider{}, ErrVersionConflict
		}
		return PlatformOAuthProvider{}, err
	}
	return row.provider()
}

func (d *DB) GetPlatformOAuthProviderByBuiltinKey(ctx context.Context, builtinKey string) (PlatformOAuthProvider, error) {
	row, err := NewPlatformOAuthProviderMapper(d.mapperDB).FindByBuiltinKey(ctx, builtinKey)
	if err != nil {
		return PlatformOAuthProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) GetPlatformOAuthProviderByUUID(ctx context.Context, providerUUID string) (PlatformOAuthProvider, error) {
	row, err := NewPlatformOAuthProviderMapper(d.mapperDB).FindByUUID(ctx, providerUUID)
	if err != nil {
		return PlatformOAuthProvider{}, mapNoRows(err)
	}
	return row.provider()
}

func (d *DB) DeletePlatformOAuthProvider(ctx context.Context, providerUUID string, expectedVersion int64) error {
	rowsAffected, err := NewPlatformOAuthProviderMapper(d.mapperDB).DeleteByUUID(ctx, providerUUID, expectedVersion)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrVersionConflict
	}
	return nil
}

func platformOAuthProviderUpsertParams(provider PlatformOAuthProvider) (upsertPlatformOAuthProviderParams, error) {
	if (provider.ClientIDEnvelope == nil || provider.ClientSecretEnvelope == nil) && !provider.Protected {
		return upsertPlatformOAuthProviderParams{}, ErrIncompleteSecretEnvelope
	}
	if (provider.ClientIDEnvelope == nil) != (provider.ClientSecretEnvelope == nil) {
		return upsertPlatformOAuthProviderParams{}, ErrIncompleteSecretEnvelope
	}
	idEnvelope, err := encodeOAuthEnvelope(provider.ClientIDEnvelope)
	if err != nil {
		return upsertPlatformOAuthProviderParams{}, err
	}
	secretEnvelope, err := encodeOAuthEnvelope(provider.ClientSecretEnvelope)
	if err != nil {
		return upsertPlatformOAuthProviderParams{}, err
	}
	return upsertPlatformOAuthProviderParams{
		UUID:                       provider.UUID,
		ExternalID:                 provider.ExternalID,
		Name:                       provider.Name,
		AuthorizeEndpoint:          provider.AuthorizeEndpoint,
		TokenEndpoint:              provider.TokenEndpoint,
		Scope:                      provider.Scope,
		ClientIDEnvelope:           idEnvelope,
		Version:                    provider.Version,
		ClientSecretEnvelope:       secretEnvelope,
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
		BuiltinKey:                 r.BuiltinKey.String,
		Protected:                  r.Protected,
		Version:                    r.Version,
		DefaultSecretName:          r.DefaultSecretName,
		TokenEndpointAuthMethod:    r.TokenEndpointAuthMethod,
		TokenEndpointEncoding:      r.TokenEndpointEncoding,
		CreatedByPlatformAdminUUID: r.CreatedByPlatformAdminUUID.String,
		CreatedAt:                  r.CreatedAt,
		UpdatedAt:                  r.UpdatedAt,
	}
	var err error
	provider.ClientIDEnvelope, err = decodeOAuthEnvelope(r.ClientIDEnvelope)
	if err != nil {
		return PlatformOAuthProvider{}, err
	}
	provider.ClientSecretEnvelope, err = decodeOAuthEnvelope(r.ClientSecretEnvelope)
	if err != nil {
		return PlatformOAuthProvider{}, err
	}
	if err := json.Unmarshal([]byte(r.DefaultAllowedHosts), &provider.DefaultAllowedHosts); err != nil {
		return PlatformOAuthProvider{}, fmt.Errorf("decode oauth default hosts: %w", err)
	}
	return provider, nil
}

func encodeOAuthEnvelope(envelope *secrets.Envelope) (*string, error) {
	if envelope == nil {
		return nil, nil
	}
	if err := requireCompleteSecretEnvelope(envelope); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return new(string(encoded)), nil
}

func decodeOAuthEnvelope(value sql.NullString) (*secrets.Envelope, error) {
	if !value.Valid {
		return nil, nil
	}
	var envelope secrets.Envelope
	if err := json.Unmarshal([]byte(value.String), &envelope); err != nil {
		return nil, fmt.Errorf("decode oauth envelope: %w", err)
	}
	if err := requireCompleteSecretEnvelope(&envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func auditBuiltinOAuthSeed(ctx context.Context, logger *slog.Logger, provider PlatformOAuthProvider) {
	logger.InfoContext(ctx, "platform oauth provider audit", "actor_type", "system", "provider_uuid", provider.UUID,
		"provider_key", "builtin:feishu", "action", "seed", "changed_fields", []string{"builtin_key", "protected"}, "result", "success")
}

func (d *DB) auditOAuthSeedAfterMigrations(ctx context.Context, before int64) error {
	if before >= 118 {
		return nil
	}
	after, err := d.OAuthMigrationVersion(ctx)
	if err != nil {
		return err
	}
	if after < 118 {
		return nil
	}
	provider, err := d.GetPlatformOAuthProviderByBuiltinKey(ctx, "feishu")
	if err != nil {
		return err
	}
	auditBuiltinOAuthSeed(ctx, d.logger, provider)
	return nil
}
