package workbench

import (
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

func TestPlatformWorkbenchCreatorUsesStableAccount(t *testing.T) {
	for _, session := range []string{"login-one", "login-two"} {
		p := auth.Principal{AccountUUID: "account", AccountExternalID: "user_account", PlatformSessionExternalID: session,
			OrganizationUUID: "target", CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin,
			WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
		r := httptest.NewRequest("GET", "/", nil).WithContext(auth.WithPrincipal(t.Context(), p))
		creator := workbenchCreator(r)
		if creator["uuid"] != "account" || creator["tagged_id"] != "user_account" || creator["email_address"] != "" {
			t.Fatalf("创建者不应使用登录会话或伪造邮箱：%+v", creator)
		}
	}
}
