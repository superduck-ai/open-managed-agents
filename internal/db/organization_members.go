package db

import (
	"context"

	"github.com/superduck-ai/yourbatis"
)

func (d *DB) withOrganizationMemberChange(ctx context.Context, organizationUUID, userID string, nextRole *string, change func(yourbatis.Executor) error) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewOrganizationMemberMapper(executor)
		if _, err := mapper.LockOrganization(ctx, organizationUUID); err != nil {
			return mapNoRows(err)
		}
		role, err := mapper.FindRole(ctx, organizationUUID, userID)
		if err != nil {
			return mapNoRows(err)
		}
		if role == "admin" && (nextRole == nil || *nextRole != "admin") {
			count, countErr := mapper.CountAdmins(ctx, organizationUUID)
			if countErr != nil {
				return countErr
			}
			if count <= 1 {
				return ErrLastOrganizationAdmin
			}
		}
		return change(executor)
	})
}

func validateOrganizationMemberActor(ctx context.Context, executor yourbatis.Executor, organizationUUID, actorID string, validate func(string) error) error {
	role, err := NewOrganizationMemberMapper(executor).FindRole(ctx, organizationUUID, actorID)
	if err != nil {
		return mapNoRows(err)
	}
	return validate(role)
}

func (d *DB) FindOrgMemberByReference(ctx context.Context, organizationUUID, userReference string) (AdminUser, error) {
	if d == nil || d.mapperDB == nil || organizationUUID == "" || userReference == "" {
		return AdminUser{}, ErrNotFound
	}
	user, err := NewOrganizationMemberMapper(d.mapperDB).FindMemberByReference(ctx, organizationUUID, userReference)
	return user, mapNoRows(err)
}
