package db

import (
	"context"
	"database/sql"
	"errors"
)

func (d *DB) ListPlatformAdminOrganizations(ctx context.Context, query PlatformAdminPageQuery) ([]PlatformAdminOrganization, error) {
	return NewPlatformAdminOrganizationMapper(d.mapperDB).List(ctx, query)
}

func (d *DB) CheckPlatformAdminOrganization(ctx context.Context, organizationUUID string) error {
	found, err := NewPlatformAdminOrganizationMapper(d.mapperDB).Exists(ctx, organizationUUID)
	if err == nil && !found {
		return ErrNotFound
	}
	return err
}

func (d *DB) GetPlatformAdminOrganization(ctx context.Context, organizationUUID string) (PlatformAdminOrganization, error) {
	row, err := NewPlatformAdminOrganizationMapper(d.mapperDB).Find(ctx, organizationUUID)
	return row, mapNoRows(err)
}

func (d *DB) ListPlatformAdminMembers(ctx context.Context, query PlatformAdminPageQuery) ([]PlatformAdminMember, error) {
	return NewPlatformAdminOrganizationMapper(d.mapperDB).ListMembers(ctx, query)
}

func (d *DB) ListPlatformAdminWorkspaces(ctx context.Context, query PlatformAdminPageQuery) ([]PlatformAdminWorkspace, error) {
	return NewPlatformAdminOrganizationMapper(d.mapperDB).ListWorkspaces(ctx, query)
}

func (tx *PlatformAdminTx) LockOrganization(ctx context.Context, organizationUUID string) (int64, error) {
	version, err := NewPlatformAdminOrganizationMapper(tx.executor).Lock(ctx, organizationUUID)
	return version, mapNoRows(err)
}

func (tx *PlatformAdminTx) Organization(ctx context.Context, organizationUUID string) (PlatformAdminOrganization, error) {
	row, err := NewPlatformAdminOrganizationMapper(tx.executor).Find(ctx, organizationUUID)
	return row, mapNoRows(err)
}

func (tx *PlatformAdminTx) CreateOrganization(ctx context.Context, input PlatformAdminOrganizationInput, workspace PlatformAuthWorkspaceInput) (PlatformAdminOrganizationCreation, bool, error) {
	mapper := NewPlatformAdminOrganizationMapper(tx.executor)
	org, err := mapper.Insert(ctx, input)
	if errors.Is(err, sql.ErrNoRows) {
		org, err = mapper.FindCreation(ctx, input.AccountUUID, input.Key)
		return org, false, err
	}
	if err != nil {
		return org, false, err
	}
	workspace.OrganizationUUID = org.UUID
	_, err = (PlatformAuthTx{executor: tx.executor}).InsertWorkspace(ctx, workspace)
	return org, true, err
}

func (tx *PlatformAdminTx) UpdateOrganization(ctx context.Context, patch PlatformAdminOrganizationPatch) error {
	return NewPlatformAdminOrganizationMapper(tx.executor).Update(ctx, patch)
}

func (tx *PlatformAdminTx) Member(ctx context.Context, organizationUUID, memberUUID string) (PlatformAdminMember, error) {
	row, err := NewPlatformAdminOrganizationMapper(tx.executor).FindMember(ctx, organizationUUID, memberUUID)
	return row, mapNoRows(err)
}

func (tx *PlatformAdminTx) MemberByEmail(ctx context.Context, organizationUUID, email string) (PlatformAdminMember, bool, error) {
	return NewPlatformAdminOrganizationMapper(tx.executor).FindMemberByEmail(ctx, organizationUUID, email)
}

func (tx *PlatformAdminTx) InsertMember(ctx context.Context, input PlatformAdminMemberInput) error {
	return NewPlatformAdminOrganizationMapper(tx.executor).InsertMember(ctx, input)
}

func (tx *PlatformAdminTx) SetMemberRole(ctx context.Context, organizationUUID, memberUUID, role string) error {
	return NewPlatformAdminOrganizationMapper(tx.executor).SetMemberRole(ctx, organizationUUID, memberUUID, role)
}

func (tx *PlatformAdminTx) CountOrganizationAdmins(ctx context.Context, organizationUUID string) (int64, error) {
	return NewOrganizationMemberMapper(tx.executor).CountAdmins(ctx, organizationUUID)
}
