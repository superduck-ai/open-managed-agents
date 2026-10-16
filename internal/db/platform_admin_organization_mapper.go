package db

import (
	"context"
	"time"
)

//go:generate go tool sqlmapgen -dir $PWD -mapper PlatformAdminOrganizationMapper -sql ./platform_admin_organization_mapper.xml -out ./platform_admin_organization_mapper.sqlmap.gen.go -dialect postgres

type PlatformAdminOrganization struct {
	UUID           string    `db:"uuid"`
	Name           string    `db:"name"`
	Website        string    `db:"website"`
	Industry       string    `db:"industry"`
	Version        int64     `db:"management_version"`
	MemberCount    int64     `db:"member_count"`
	WorkspaceCount int64     `db:"workspace_count"`
	AdminCount     int64     `db:"admin_count"`
	CreatedAt      time.Time `db:"created_at"`
}

type PlatformAdminMember struct {
	UUID  string `db:"uuid"`
	Email string `db:"email"`
	Name  string `db:"name"`
	Role  string `db:"role"`
}

type PlatformAdminWorkspace struct {
	UUID       string `db:"uuid"`
	ExternalID string `db:"external_id"`
	Name       string `db:"name"`
	IsDefault  bool   `db:"is_default"`
	Archived   bool   `db:"archived"`
}

type PlatformAdminOrganizationPatch struct {
	OrganizationUUID string
	Name             *string
	Website          *string
	Industry         *string
}
type PlatformAdminMemberInput struct {
	OrganizationUUID string
	UUID             string
	ExternalID       string
	Email            string
	Name             string
	Role             string
}
type PlatformAdminCreateRequest struct {
	AccountUUID string
	Key         string
	Hash        string `db:"request_hash"`
	Response    []byte `db:"response"`
}

type PlatformAdminOrganizationMapper interface {
	List(ctx context.Context, params PlatformAdminPageQuery) ([]PlatformAdminOrganization, error)
	Exists(ctx context.Context, organizationUUID string) (bool, error)
	Find(ctx context.Context, organizationUUID string) (PlatformAdminOrganization, error)
	Lock(ctx context.Context, organizationUUID string) (int64, error)
	Update(ctx context.Context, params PlatformAdminOrganizationPatch) error
	ListMembers(ctx context.Context, params PlatformAdminPageQuery) ([]PlatformAdminMember, error)
	ListWorkspaces(ctx context.Context, params PlatformAdminPageQuery) ([]PlatformAdminWorkspace, error)
	FindMember(ctx context.Context, organizationUUID, memberUUID string) (PlatformAdminMember, error)
	FindMemberByEmail(ctx context.Context, organizationUUID, email string) (PlatformAdminMember, bool, error)
	InsertMember(ctx context.Context, params PlatformAdminMemberInput) error
	SetMemberRole(ctx context.Context, organizationUUID, memberUUID, role string) error
	FindCreateRequest(ctx context.Context, accountUUID, key string) (PlatformAdminCreateRequest, bool, error)
	SaveCreateRequest(ctx context.Context, params PlatformAdminCreateRequest) error
}
