package db

import (
	"context"
	"database/sql"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleOrganizationMapper -sql ./console_organizations_mapper.xml -out ./console_organizations_mapper.sqlmap.gen.go -dialect postgres

type updateConsoleOrganizationParams struct {
	OrgUUID  string
	Name     *string
	Settings []byte
}

type updateConsoleOrganizationProfileParams struct {
	OrgUUID string
	Profile []byte
}

type consoleOrganizationRow struct {
	UUID                   string         `db:"uuid"`
	Name                   string         `db:"name"`
	Domain                 *string        `db:"domain"`
	ParentOrganizationUUID sql.NullString `db:"parent_organization_uuid"`
	Settings               []byte         `db:"settings"`
	CreatedAt              time.Time      `db:"created_at"`
	UpdatedAt              time.Time      `db:"updated_at"`
	Role                   string         `db:"role"`
	AddedAt                time.Time      `db:"added_at"`
}

type consoleOrganizationProfileRow struct {
	Profile []byte `db:"profile"`
}

type ConsoleOrganizationMapper interface {
	FindByUUID(ctx context.Context, orgUUID string) (consoleOrganizationRow, error)
	UpdateByUUID(ctx context.Context, params updateConsoleOrganizationParams) (consoleOrganizationRow, error)
	FindProfileByUUID(ctx context.Context, orgUUID string) (consoleOrganizationProfileRow, error)
	UpdateProfileByUUID(ctx context.Context, params updateConsoleOrganizationProfileParams) (consoleOrganizationProfileRow, error)
}
