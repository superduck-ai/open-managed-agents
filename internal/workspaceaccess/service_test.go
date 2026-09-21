package workspaceaccess

import (
	"errors"
	"testing"
)

func TestEffective(t *testing.T) {
	tests := []struct {
		name, orgRole, explicitRole string
		isDefault                   bool
		wantRole, wantSource        string
	}{
		{name: "未知组织角色不能提权", orgRole: "unknown", explicitRole: "workspace_admin"},
		{name: "普通成员未加入", orgRole: "user"},
		{name: "默认忽略历史管理员", orgRole: "user", explicitRole: "workspace_admin", isDefault: true, wantRole: "workspace_user", wantSource: "organization"},
		{name: "默认忽略历史降权", orgRole: "admin", explicitRole: "workspace_user", isDefault: true, wantRole: "workspace_admin", wantSource: "organization"},
		{name: "默认开发者无记录", orgRole: "developer", isDefault: true, wantRole: "workspace_developer", wantSource: "organization"},
		{name: "默认代码用户", orgRole: "claude_code_user", isDefault: true, wantRole: "workspace_user", wantSource: "organization"},
		{name: "组织管理员继承", orgRole: "admin", wantRole: "workspace_admin", wantSource: "organization"},
		{name: "降级保留显式授权", orgRole: "user", explicitRole: "workspace_admin", wantRole: "workspace_admin", wantSource: "membership"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			access, err := Effective(tt.orgRole, tt.isDefault, tt.explicitRole)
			if tt.wantRole == "" {
				if !errors.Is(err, ErrDenied) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || access.Role != tt.wantRole || access.Source != tt.wantSource {
				t.Fatalf("access = %+v, err = %v", access, err)
			}
		})
	}
}

func TestMemberChangeProtection(t *testing.T) {
	if !errors.Is(validateMemberChange("admin", "", "delete"), ErrInheritedRole) {
		t.Fatal("继承管理员不能移除")
	}
}
