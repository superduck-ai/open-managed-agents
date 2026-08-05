package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type AdminWorkspaceMember struct {
	UUID                uuid.UUID `db:"uuid"`
	ExternalID          string    `db:"external_id"`
	OrganizationUUID    uuid.UUID `db:"organization_uuid"`
	WorkspaceUUID       uuid.UUID `db:"workspace_uuid"`
	WorkspaceExternalID string    `db:"workspace_external_id"`
	UserUUID            uuid.UUID `db:"user_uuid"`
	UserExternalID      string    `db:"user_external_id"`
	WorkspaceRole       string    `db:"workspace_role"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

type ListAdminMembersParams struct {
	OrganizationUUID string
	WorkspaceUUID    string
	AfterID          string
	BeforeID         string
	Limit            int
}

func (d *DB) CreateAdminWorkspaceMember(ctx context.Context, member AdminWorkspaceMember) (AdminWorkspaceMember, error) {
	mapper := NewAdminWorkspaceMemberMapper(d.mapperDB)
	created, err := mapper.Insert(ctx, insertAdminWorkspaceMemberParams{
		ExternalID:          member.ExternalID,
		OrganizationUUID:    member.OrganizationUUID,
		WorkspaceUUID:       member.WorkspaceUUID,
		WorkspaceExternalID: member.WorkspaceExternalID,
		UserUUID:            member.UserUUID,
		UserExternalID:      member.UserExternalID,
		WorkspaceRole:       member.WorkspaceRole,
		CreatedAt:           member.CreatedAt,
	})
	if isUniqueViolation(err) {
		return AdminWorkspaceMember{}, ErrDuplicate
	}
	return created, err
}

func (d *DB) GetAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	mapper := NewAdminWorkspaceMemberMapper(d.mapperDB)
	member, err := mapper.FindByUserExternalID(ctx, organizationUUID, workspaceExternalID, userExternalID)
	return member, mapNoRows(err)
}

func (d *DB) ListAdminWorkspaceMembersPage(ctx context.Context, params ListAdminMembersParams) ([]AdminWorkspaceMember, bool, error) {
	mapper := NewAdminWorkspaceMemberMapper(d.mapperDB)
	var anchor *pagePosition
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	if cursorID != "" {
		value, found, err := mapper.FindPageAnchorByUserExternalID(ctx, params.WorkspaceUUID, cursorID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		anchor = &value
	}
	before := params.AfterID == "" && params.BeforeID != ""
	members, err := mapper.ListPage(
		ctx,
		params.OrganizationUUID,
		params.WorkspaceUUID,
		anchor,
		before,
		params.Limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(members, params.Limit), len(members) > params.Limit, nil
}

func (d *DB) UpdateAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID, role string) (AdminWorkspaceMember, error) {
	mapper := NewAdminWorkspaceMemberMapper(d.mapperDB)
	member, err := mapper.UpdateRoleByUserExternalID(ctx, updateAdminWorkspaceMemberRoleParams{
		OrganizationUUID:    organizationUUID,
		WorkspaceExternalID: workspaceExternalID,
		UserExternalID:      userExternalID,
		WorkspaceRole:       role,
	})
	return member, mapNoRows(err)
}

func (d *DB) DeleteAdminWorkspaceMember(ctx context.Context, organizationUUID, workspaceExternalID, userExternalID string) (AdminWorkspaceMember, error) {
	mapper := NewAdminWorkspaceMemberMapper(d.mapperDB)
	member, err := mapper.SoftDeleteByUserExternalID(ctx, organizationUUID, workspaceExternalID, userExternalID)
	return member, mapNoRows(err)
}
