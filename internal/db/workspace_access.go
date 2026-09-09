package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

// WorkspaceMemberTx 限定成员变更所需的数据访问，所有方法使用同一个事务。
type WorkspaceMemberTx struct{ executor yourbatis.Executor }

func (d *DB) WithWorkspaceMemberTx(ctx context.Context, organizationUUID, actorID, targetID, workspaceID string, fn func(*WorkspaceMemberTx) error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		tx := &WorkspaceMemberTx{executor: executor}
		locks := NewWorkspaceAccessMapper(executor)
		if _, err := locks.LockUsers(ctx, organizationUUID, actorID, targetID); err != nil {
			return err
		}
		workspace, err := tx.GetAdminWorkspace(ctx, organizationUUID, workspaceID)
		if err != nil {
			return err
		}
		if _, err := locks.LockWorkspace(ctx, organizationUUID, workspace.UUID); err != nil {
			return mapNoRows(err)
		}
		return fn(tx)
	})
}

func (tx *WorkspaceMemberTx) GetAdminUser(ctx context.Context, orgUUID, userID string) (AdminUser, error) {
	value, err := NewAdminUserMapper(tx.executor).FindByExternalID(ctx, orgUUID, userID)
	return value, mapNoRows(err)
}
func (tx *WorkspaceMemberTx) GetAdminWorkspace(ctx context.Context, orgUUID, workspaceID string) (AdminWorkspace, error) {
	value, err := NewAdminWorkspaceMapper(tx.executor).FindByIdentifier(ctx, orgUUID, workspaceID, tryParseDBUUIDIdentifierString(workspaceID))
	return value, mapNoRows(err)
}
func (tx *WorkspaceMemberTx) GetAdminWorkspaceMember(ctx context.Context, orgUUID, workspaceID, userID string) (AdminWorkspaceMember, error) {
	value, err := NewAdminWorkspaceMemberMapper(tx.executor).FindByUserExternalID(ctx, orgUUID, workspaceID, userID)
	return value, mapNoRows(err)
}
func (tx *WorkspaceMemberTx) InsertMember(ctx context.Context, member AdminWorkspaceMember) (AdminWorkspaceMember, error) {
	value, err := NewAdminWorkspaceMemberMapper(tx.executor).Insert(ctx, insertAdminWorkspaceMemberParams{
		ExternalID: member.ExternalID, OrganizationUUID: member.OrganizationUUID, WorkspaceUUID: member.WorkspaceUUID,
		WorkspaceExternalID: member.WorkspaceExternalID, UserUUID: member.UserUUID, UserExternalID: member.UserExternalID,
		WorkspaceRole: member.WorkspaceRole, CreatedAt: member.CreatedAt,
	})
	if isUniqueViolation(err) {
		return AdminWorkspaceMember{}, ErrDuplicate
	}
	return value, err
}
func (tx *WorkspaceMemberTx) UpdateMember(ctx context.Context, orgUUID, workspaceID, userID, role string) (AdminWorkspaceMember, error) {
	value, err := NewAdminWorkspaceMemberMapper(tx.executor).UpdateRoleByUserExternalID(ctx, updateAdminWorkspaceMemberRoleParams{
		OrganizationUUID: orgUUID, WorkspaceExternalID: workspaceID, UserExternalID: userID, WorkspaceRole: role,
	})
	return value, mapNoRows(err)
}
func (tx *WorkspaceMemberTx) DeleteMember(ctx context.Context, orgUUID, workspaceID, userID string) (AdminWorkspaceMember, error) {
	value, err := NewAdminWorkspaceMemberMapper(tx.executor).SoftDeleteByUserExternalID(ctx, orgUUID, workspaceID, userID)
	return value, mapNoRows(err)
}
func (d *DB) ListWorkspaceMemberFacts(ctx context.Context, orgUUID, workspaceUUID string) ([]WorkspaceMemberFact, error) {
	return NewWorkspaceAccessMapper(d.mapperDB).ListMemberFacts(ctx, orgUUID, workspaceUUID)
}

func (d *DB) ListUserWorkspaceRoles(ctx context.Context, organizationUUID, userUUID string) ([]WorkspaceRoleFact, error) {
	return NewWorkspaceAccessMapper(d.mapperDB).ListUserRoles(ctx, organizationUUID, userUUID)
}
