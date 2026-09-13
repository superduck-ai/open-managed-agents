package platformapi

import (
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/workspaceaccess"
)

func TestConsoleWorkspaceMemberPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, orgRole, explicit string
		manage, edit, remove    bool
	}{
		{"只读操作者", "user", "workspace_user", false, false, false},
		{"继承管理员", "admin", "", true, false, false},
		{"继承计费可提权", "billing", "", true, true, false},
		{"提权计费可恢复但不可移除", "billing", "workspace_admin", true, true, false},
		{"显式成员", "user", "workspace_user", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			access, err := workspaceaccess.Effective(tc.orgRole, false, tc.explicit)
			if err != nil {
				t.Fatal(err)
			}
			result := formatConsoleWorkspaceMember(db.WorkspaceMemberFact{OrganizationRole: tc.orgRole, Email: "member@example.com"}, access, tc.manage)
			if result["can_edit"] != tc.edit || result["can_remove"] != tc.remove {
				t.Fatalf("unexpected permissions: %v", result)
			}
		})
	}
}

func TestConsoleWorkspaceMemberName(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"", "member@example.com"},
		{"   ", "member@example.com"},
		{"成员", "成员"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := formatConsoleWorkspaceMember(db.WorkspaceMemberFact{Name: tc.name, Email: "member@example.com"}, auth.WorkspaceAccess{}, false)
			if result["name"] != tc.want || result["email"] != "member@example.com" {
				t.Fatalf("成员资料不匹配: %v", result)
			}
		})
	}
}
