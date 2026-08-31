package db

import (
	"context"
	"encoding/json"
	"time"
)

type AdminExternalKey struct {
	UUID             string          `db:"uuid"`
	ExternalID       string          `db:"external_id"`
	OrganizationUUID string          `db:"organization_uuid"`
	DisplayName      string          `db:"display_name"`
	Geo              string          `db:"geo"`
	ProviderConfig   json.RawMessage `db:"provider_config"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

type ListAdminExternalKeysParams struct {
	OrganizationUUID string
	Limit            int
	Offset           int
}

func (d *DB) CreateAdminExternalKey(ctx context.Context, key AdminExternalKey) (AdminExternalKey, error) {
	mapper := NewAdminExternalKeyMapper(d.mapperDB)
	created, err := mapper.Insert(ctx, insertAdminExternalKeyParams{
		ExternalID:       key.ExternalID,
		OrganizationUUID: key.OrganizationUUID,
		DisplayName:      key.DisplayName,
		Geo:              key.Geo,
		ProviderConfig:   key.ProviderConfig,
		CreatedAt:        key.CreatedAt,
	})
	if isUniqueViolation(err) {
		return AdminExternalKey{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminExternalKey(ctx context.Context, organizationUUID, externalID string) (AdminExternalKey, error) {
	mapper := NewAdminExternalKeyMapper(d.mapperDB)
	key, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
	return key, mapNoRows(err)
}

func (d *DB) ListAdminExternalKeysPage(ctx context.Context, params ListAdminExternalKeysParams) ([]AdminExternalKey, bool, error) {
	mapper := NewAdminExternalKeyMapper(d.mapperDB)
	keys, err := mapper.ListPage(
		ctx,
		params.OrganizationUUID,
		params.Limit+1,
		params.Offset,
	)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminExternalKey(ctx context.Context, organizationUUID, externalID string, next AdminExternalKey) (AdminExternalKey, error) {
	mapper := NewAdminExternalKeyMapper(d.mapperDB)
	updated, err := mapper.UpdateByExternalID(ctx, updateAdminExternalKeyParams{
		OrganizationUUID: organizationUUID,
		ExternalID:       externalID,
		DisplayName:      next.DisplayName,
		Geo:              next.Geo,
		ProviderConfig:   next.ProviderConfig,
		UpdatedAt:        next.UpdatedAt,
	})
	return updated, mapNoRows(err)
}

func (d *DB) DeleteAdminExternalKey(ctx context.Context, organizationUUID, externalID string) error {
	mapper := NewAdminExternalKeyMapper(d.mapperDB)
	affected, err := mapper.SoftDeleteByExternalID(ctx, organizationUUID, externalID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
