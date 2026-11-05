package channels

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestChannelCredentialsPreservePlatformRuntimeAccount(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer upstream.Close()
	for _, credential := range []string{auth.CredentialTypePlatformSession, auth.CredentialTypeAPIKey} {
		t.Run(credential, func(t *testing.T) {
			store := &provisioningStore{}
			h := handler{store: store, cfg: config.ChannelBridgeConfig{URL: upstream.URL, Token: "test-token"}, client: upstream.Client()}
			p := auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a", AccountUUID: "stable-account",
				CredentialType: credential, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin,
				WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
			r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.syncWorkspaceCredentials(r, p); err != nil {
				t.Fatal(err)
			}
			if store.input.RuntimeUserUUID != "stable-account" || store.input.CreatedByUserUUID != nil {
				t.Fatalf("渠道运行账号丢失或冒用成员：%+v", store.input)
			}
		})
	}
}
