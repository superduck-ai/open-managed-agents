package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAdminMapper -sql ./platform_admin_mapper.xml -out ./platform_admin_mapper.sqlmap.gen.go -dialect postgres

type PlatformAdminAccount struct {
	UUID    string `db:"uuid"`
	Email   string `db:"email"`
	Name    string `db:"name"`
	Granted bool   `db:"granted"`
}

type PlatformAdminGrant struct {
	AccountUUID string
	Operator    string
}

type PlatformAdminPageQuery struct {
	Query            string
	After            string
	Limit            int
	OrganizationUUID string
}

type PlatformAdminMapper interface {
	FindAccount(ctx context.Context, accountUUID string) (PlatformAdminAccount, error)
	ListAccounts(ctx context.Context, params PlatformAdminPageQuery) ([]PlatformAdminAccount, error)
	ListAdmins(ctx context.Context) ([]PlatformAdminAccount, error)
	LockGrant(ctx context.Context, accountUUID string) (bool, error)
	LockAdministration(ctx context.Context) (int64, error)
	CountAdmins(ctx context.Context) (int64, error)
	Grant(ctx context.Context, params PlatformAdminGrant) error
	Revoke(ctx context.Context, params PlatformAdminGrant) error
}
