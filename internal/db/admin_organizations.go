package db

import (
	"context"
	"time"
	"uuid"
)

type AdminOrganization struct {
	UUID      string    `db:"uuid"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

func (d *DB) GetAdminOrganization(ctx context.Context, organizationUUID uuid.UUID) (AdminOrganization, error) {
	mapper := NewAdminOrganizationMapper(d.mapperDB)
	organization, err := mapper.FindByUUID(ctx, organizationUUID.String())
	return organization, mapNoRows(err)
}
