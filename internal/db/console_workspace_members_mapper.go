package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper ConsoleWorkspaceMemberMapper -sql ./console_workspace_members_mapper.xml -out ./console_workspace_members_mapper.sqlmap.gen.go -dialect postgres

type ConsoleWorkspaceMemberMapper interface {
	SoftDeleteByOrganizationUser(ctx context.Context, params consoleUserIdentifierParams) error
}
