package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleUserMapper -sql ./console_users_mapper.xml -out ./console_users_mapper.sqlmap.gen.go -dialect postgres

type consoleUserIdentifierParams struct {
	OrgUUID  string
	UserID   string
	UserUUID string
}

type updateConsoleUserRoleParams struct {
	OrgUUID  string
	UserID   string
	UserUUID string
	Role     string
}

type consoleMemberRow struct {
	UserUUID string    `db:"user_uuid"`
	Email    string    `db:"email"`
	FullName *string   `db:"full_name"`
	Role     string    `db:"role"`
	AddedAt  time.Time `db:"added_at"`
}

type bootstrapUserContextRow struct {
	UserExternalID string `db:"user_external_id"`
	OrgUUID        string `db:"org_uuid"`
}

type bootstrapUserRow struct {
	UUID          string    `db:"uuid"`
	ExternalID    string    `db:"external_id"`
	Email         string    `db:"email"`
	FullName      *string   `db:"full_name"`
	DisplayName   *string   `db:"display_name"`
	IsVerified    bool      `db:"is_verified"`
	AgeIsVerified bool      `db:"age_is_verified"`
	CreatedAt     time.Time `db:"created_at"`
}

type ConsoleUserMapper interface {
	ExistsActiveByUUID(ctx context.Context, orgUUID, userUUID string) (bool, error)
	ListOrganizationMembers(ctx context.Context, orgUUID string, limit int) ([]consoleMemberRow, error)
	UpdateOrganizationRole(ctx context.Context, params updateConsoleUserRoleParams) (consoleMemberRow, error)
	SoftDeleteOrganizationMember(ctx context.Context, params consoleUserIdentifierParams) (int64, error)
	FindBootstrapContext(ctx context.Context, preferredOrgUUID string) (bootstrapUserContextRow, error)
	FindBootstrapUser(ctx context.Context, userExternalID, userUUID string) (bootstrapUserRow, error)
	ListBootstrapOrganizations(ctx context.Context, userExternalID, userUUID, preferredOrgUUID string) ([]consoleOrganizationRow, error)
}
