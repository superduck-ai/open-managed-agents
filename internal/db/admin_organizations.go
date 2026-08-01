package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminOrganization struct {
	UUID      uuid.UUID `db:"uuid"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

const getAdminOrganizationQuery = `
	select uuid, name, created_at
	from organizations
	where uuid = :organization_uuid
`

func (d *DB) GetAdminOrganization(ctx context.Context, organizationUUID uuid.UUID) (AdminOrganization, error) {
	return getAdminRow[AdminOrganization](ctx, d.sql, getAdminOrganizationQuery, map[string]any{
		"organization_uuid": organizationUUID,
	})
}
