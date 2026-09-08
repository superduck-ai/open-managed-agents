package db

import "context"

//go:generate go tool sqlmapgen -dir $PWD -mapper WorkspaceAccessMapper -sql ./workspace_access.xml -out ./workspace_access.sqlmap.gen.go -dialect postgres

type WorkspaceMemberFact struct {
	UserUUID         string `db:"user_uuid"`
	UserExternalID   string `db:"user_external_id"`
	OrganizationRole string `db:"organization_role"`
	ExplicitRole     string `db:"explicit_role"`
}

type WorkspaceAccessMapper interface {
	LockUsers(ctx context.Context, organizationUUID, actorID, targetID string) ([]AdminUser, error)
	LockWorkspace(ctx context.Context, organizationUUID, workspaceUUID string) (string, error)
	ListMemberFacts(ctx context.Context, organizationUUID, workspaceUUID string) ([]WorkspaceMemberFact, error)
}
