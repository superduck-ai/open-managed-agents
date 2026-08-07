package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
)

type AdminUser struct {
	UUID             uuid.UUID `db:"uuid"`
	ExternalID       string    `db:"external_id"`
	OrganizationUUID uuid.UUID `db:"organization_uuid"`
	Email            string    `db:"email"`
	Name             string    `db:"name"`
	Role             string    `db:"role"`
	AddedAt          time.Time `db:"added_at"`
}

type ListAdminUsersParams struct {
	OrganizationUUID string
	Email            string
	AfterID          string
	BeforeID         string
	Limit            int
}

func (d *DB) GetAdminUser(ctx context.Context, organizationUUID, externalID string) (AdminUser, error) {
	mapper := NewAdminUserMapper(d.mapperDB)
	user, err := mapper.FindByExternalID(ctx, organizationUUID, externalID)
	return user, mapNoRows(err)
}

func (d *DB) ListAdminUsersPage(ctx context.Context, params ListAdminUsersParams) ([]AdminUser, bool, error) {
	mapper := NewAdminUserMapper(d.mapperDB)
	var anchor *pagePosition
	cursorID := firstNonEmpty(params.AfterID, params.BeforeID)
	if cursorID != "" {
		value, found, err := mapper.FindPageAnchorByExternalID(ctx, params.OrganizationUUID, cursorID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			return nil, false, nil
		}
		anchor = &value
	}
	before := params.AfterID == "" && params.BeforeID != ""
	users, err := mapper.ListPage(
		ctx,
		params.OrganizationUUID,
		params.Email,
		anchor,
		before,
		params.Limit+1,
	)
	if err != nil {
		return nil, false, err
	}
	return trimAdminPage(users, params.Limit), len(users) > params.Limit, nil
}

func (d *DB) UpdateAdminUserRole(ctx context.Context, organizationUUID, externalID, role string) (AdminUser, error) {
	mapper := NewAdminUserMapper(d.mapperDB)
	user, err := mapper.UpdateRoleByExternalID(ctx, organizationUUID, externalID, role)
	return user, mapNoRows(err)
}

func (d *DB) DeleteAdminUser(ctx context.Context, organizationUUID, externalID string) (AdminUser, error) {
	var user AdminUser
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewAdminUserMapper(executor)
		deleted, deleteErr := mapper.SoftDeleteByExternalID(ctx, organizationUUID, externalID)
		if deleteErr != nil {
			return mapNoRows(deleteErr)
		}
		if deleteErr = mapper.SoftDeleteWorkspaceMembersByUserUUID(
			ctx,
			organizationUUID,
			deleted.UUID.String(),
		); deleteErr != nil {
			return deleteErr
		}
		user = deleted
		return nil
	})
	if err != nil {
		return AdminUser{}, err
	}
	return user, nil
}
