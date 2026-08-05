package db

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/yourbatis"
)

func (d *DB) ListOrgUsers(ctx context.Context, orgUUID string, limit int) ([]platform.OrgUser, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" {
		return []platform.OrgUser{}, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	mapper := NewConsoleUserMapper(d.mapperDB)
	rows, err := mapper.ListOrganizationMembers(ctx, orgUUID, limit)
	if err != nil {
		return nil, err
	}

	out := make([]platform.OrgUser, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.user())
	}
	return out, nil
}

func (d *DB) UpdateOrgUserRole(ctx context.Context, orgUUID string, userID string, role string) (*platform.OrgUser, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" || userID == "" {
		return nil, nil
	}
	mapper := NewConsoleUserMapper(d.mapperDB)
	row, err := mapper.UpdateOrganizationRole(ctx, updateConsoleUserRoleParams{
		OrgUUID:  orgUUID,
		UserID:   userID,
		UserUUID: tryParseDBUUIDIdentifierString(userID),
		Role:     role,
	})
	if err != nil {
		err = mapNoRows(err)
		if errors.Is(err, platform.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	user := row.user()
	return &user, nil
}

func (d *DB) RemoveOrgUser(ctx context.Context, orgUUID string, userID string) (bool, error) {
	if d == nil || d.mapperDB == nil || orgUUID == "" || userID == "" {
		return false, nil
	}
	params := consoleUserIdentifierParams{
		OrgUUID:  orgUUID,
		UserID:   userID,
		UserUUID: tryParseDBUUIDIdentifierString(userID),
	}
	removed := false
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		userMapper := NewConsoleUserMapper(executor)
		workspaceMemberMapper := NewConsoleWorkspaceMemberMapper(executor)
		rowsAffected, err := userMapper.SoftDeleteOrganizationMember(ctx, params)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return nil
		}
		if err := workspaceMemberMapper.SoftDeleteByOrganizationUser(ctx, params); err != nil {
			return err
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (r consoleMemberRow) user() platform.OrgUser {
	return platform.OrgUser{
		UserUUID: r.UserUUID,
		Email:    r.Email,
		FullName: r.FullName,
		Role:     r.Role,
		AddedAt:  r.AddedAt,
	}
}
