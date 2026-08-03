package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type AdminExternalKey struct {
	UUID             uuid.UUID       `db:"uuid"`
	ExternalID       string          `db:"external_id"`
	OrganizationUUID uuid.UUID       `db:"organization_uuid"`
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
	created, err := getAdminRow[AdminExternalKey](ctx, d.sql, `
		insert into external_keys (
			external_id, organization_uuid, display_name, geo, provider_config, created_at, updated_at
		)
		values (
			:external_id, :organization_uuid, :display_name, :geo,
			CAST(:provider_config AS jsonb), :created_at, :created_at
		)
		returning uuid, external_id,
			organization_uuid,
			display_name, geo, provider_config, created_at, updated_at
	`, adminExternalKeyArguments(key))
	if isUniqueViolation(err) {
		return AdminExternalKey{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminExternalKey(ctx context.Context, organizationUUID, externalID string) (AdminExternalKey, error) {
	return getAdminRow[AdminExternalKey](ctx, d.sql, adminExternalKeySelectSQL()+`
		where organization_uuid = :organization_uuid
			and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
}

func (d *DB) ListAdminExternalKeysPage(ctx context.Context, params ListAdminExternalKeysParams) ([]AdminExternalKey, bool, error) {
	keys, err := selectAdminRows[AdminExternalKey](ctx, d.sql, adminExternalKeySelectSQL()+`
		where organization_uuid = :organization_uuid and deleted_at is null
		order by created_at desc, uuid desc
		limit :limit offset :offset
	`, map[string]any{
		"organization_uuid": dbUUID(params.OrganizationUUID),
		"limit":             params.Limit + 1,
		"offset":            params.Offset,
	})
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(keys, params.Limit), len(keys) > params.Limit, nil
}

func (d *DB) UpdateAdminExternalKey(ctx context.Context, organizationUUID, externalID string, next AdminExternalKey) (AdminExternalKey, error) {
	args := adminExternalKeyArguments(next)
	args["organization_uuid"] = dbUUID(organizationUUID)
	args["external_id"] = externalID
	return getAdminRow[AdminExternalKey](ctx, d.sql, `
		update external_keys
		set display_name = :display_name,
			geo = :geo,
			provider_config = CAST(:provider_config AS jsonb),
			updated_at = :updated_at
		where organization_uuid = :organization_uuid
			and external_id = :external_id and deleted_at is null
		returning uuid, external_id,
			organization_uuid,
			display_name, geo, provider_config, created_at, updated_at
	`, args)
}

func (d *DB) DeleteAdminExternalKey(ctx context.Context, organizationUUID, externalID string) error {
	affected, err := namedExecRowsAffected(ctx, d.sql, `
		update external_keys
		set deleted_at = coalesce(deleted_at, now()),
			updated_at = now()
		where organization_uuid = :organization_uuid
			and external_id = :external_id and deleted_at is null
	`, map[string]any{"organization_uuid": dbUUID(organizationUUID), "external_id": externalID})
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func adminExternalKeySelectSQL() string {
	return `
		select uuid, external_id,
			organization_uuid,
			display_name, geo, provider_config, created_at, updated_at
		from external_keys
	`
}

func adminExternalKeyArguments(key AdminExternalKey) map[string]any {
	return map[string]any{
		"external_id":       key.ExternalID,
		"organization_uuid": key.OrganizationUUID,
		"display_name":      key.DisplayName,
		"geo":               key.Geo,
		"provider_config":   jsonArg(key.ProviderConfig),
		"created_at":        key.CreatedAt,
		"updated_at":        key.UpdatedAt,
	}
}
