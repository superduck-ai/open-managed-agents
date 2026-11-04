package workspaceaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type platformWorkspaceStore struct {
	workspace   db.AdminWorkspace
	err         error
	memberCalls int
}

func (s *platformWorkspaceStore) GetAdminUser(context.Context, string, string) (db.AdminUser, error) {
	s.memberCalls++
	return db.AdminUser{}, db.ErrNotFound
}
func (s *platformWorkspaceStore) GetAdminWorkspace(context.Context, string, string) (db.AdminWorkspace, error) {
	return s.workspace, s.err
}
func (s *platformWorkspaceStore) GetAdminWorkspaceMember(context.Context, string, string, string) (db.AdminWorkspaceMember, error) {
	s.memberCalls++
	return db.AdminWorkspaceMember{}, db.ErrNotFound
}

func TestResolvePlatformPrincipal(t *testing.T) {
	principal := auth.Principal{AccountUUID: "account", OrganizationUUID: "org", CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin, WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
	for _, tc := range []struct {
		name, org           string
		archived, isDefault bool
		err                 error
		allowed             bool
	}{
		{name: "第三组织", org: "other"},
		{name: "归档拒绝", org: "org", archived: true},
		{name: "不存在", err: db.ErrNotFound},
		{name: "故障不放行", err: errors.New("database unavailable")},
		{name: "默认工作区", org: "org", isDefault: true, allowed: true},
		{name: "非默认无需成员", org: "org", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &platformWorkspaceStore{workspace: db.AdminWorkspace{UUID: "workspace", OrganizationUUID: tc.org, IsDefault: tc.isDefault}, err: tc.err}
			if tc.archived {
				store.workspace.ArchivedAt = new(time.Now())
			}
			_, access, err := New(store).ResolvePrincipal(t.Context(), principal, "workspace")
			if (err == nil) != tc.allowed || store.memberCalls != 0 {
				t.Fatalf("access=%+v err=%v member_calls=%d", access, err, store.memberCalls)
			}
			if tc.allowed && (!access.ManageOrganization() || !access.ManageMembers() || access.Source != "platform_admin") {
				t.Fatalf("access=%+v", access)
			}
		})
	}
	principal.OrganizationAccessSource = auth.OrganizationAccessMember
	if _, _, err := New(&platformWorkspaceStore{}).ResolvePrincipal(t.Context(), principal, "workspace"); !errors.Is(err, ErrDenied) {
		t.Fatal("空成员普通来源不应通过")
	}
}
