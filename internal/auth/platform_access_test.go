package auth

import "testing"

func TestPlatformOrganizationAdmin(t *testing.T) {
	base := Principal{AccountUUID: "account", AccountExternalID: "user_account", OrganizationUUID: "target", CredentialType: CredentialTypePlatformSession, OrganizationAccessSource: OrganizationAccessPlatformAdmin, WorkspaceAccess: WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
	for _, tc := range []struct {
		name    string
		change  func(*Principal)
		target  string
		allowed bool
	}{
		{"空成员不是授权", func(p *Principal) { p.OrganizationAccessSource = "" }, "target", false},
		{"服务凭据不能提权", func(p *Principal) { p.CredentialType = CredentialTypeAPIKey }, "target", false},
		{"缺少稳定账号", func(p *Principal) { p.AccountUUID = "" }, "target", false},
		{"伪造角色不够", func(p *Principal) { p.WorkspaceAccess.Source = "organization" }, "target", false},
		{"第三组织拒绝", func(*Principal) {}, "other", false},
		{"有效平台来源", func(*Principal) {}, "target", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			principal := base
			tc.change(&principal)
			if principal.PlatformOrganizationAdmin(tc.target) != tc.allowed {
				t.Fatal("组织授权判断错误")
			}
			if tc.allowed && (principal.RuntimeUserUUID() != "account" || principal.ActorUserID() != "user_account" || principal.UserUUID != "") {
				t.Fatal("运行身份不应伪造成员")
			}
		})
	}
	member := Principal{UserUUID: "member", UserExternalID: "user_member"}
	if member.RuntimeUserUUID() != "member" || member.ActorUserID() != "user_member" {
		t.Fatal("普通成员身份发生改变")
	}
}
