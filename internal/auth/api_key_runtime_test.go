package auth

import "testing"

func TestAPIKeyRuntimeIdentityDoesNotGrantPlatformAccess(t *testing.T) {
	for _, tc := range []struct {
		name, account, want string
	}{
		{"没有运行账号不借用组织成员", "", ""},
		{"独立运行账号", "account", "account"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Principal{CredentialType: CredentialTypeAPIKey, AccountUUID: tc.account, UserUUID: "member", OrganizationUUID: "target"}
			if p.RuntimeUserUUID() != tc.want || p.PlatformOrganizationAdmin("target") || p.WorkspaceAccess.ManageOrganization() {
				t.Fatalf("运行身份或权限错误：%+v", p)
			}
		})
	}
}
