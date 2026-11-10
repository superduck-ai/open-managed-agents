package channels

import (
	"errors"
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

func TestChannelCredentialsResolvePlatformMemberCreator(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer upstream.Close()
	for _, tc := range []struct{ name, member string }{
		{"目标组织存在同邮箱成员时保存成员创建者", "member-uuid"},
		{"目标组织没有成员时不冒用账号", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &provisioningStore{creatorMember: tc.member}
			h := handler{store: store, cfg: config.ChannelBridgeConfig{URL: upstream.URL, Token: "test-token"}, client: upstream.Client()}
			p := auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a", AccountUUID: "stable-account",
				CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin,
				WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
			r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.syncWorkspaceCredentials(r, p); err != nil {
				t.Fatal(err)
			}
			if store.input.RuntimeUserUUID != "stable-account" {
				t.Fatalf("渠道运行账号丢失：%+v", store.input)
			}
			if tc.member == "" && store.input.CreatedByUserUUID != nil {
				t.Fatalf("目标组织没有成员时不应保存创建者：%+v", store.input)
			}
			if tc.member != "" && (store.input.CreatedByUserUUID == nil || *store.input.CreatedByUserUUID != tc.member) {
				t.Fatalf("渠道创建者 = %v，预期 %q", store.input.CreatedByUserUUID, tc.member)
			}
		})
	}
}

func TestChannelCredentialsRejectCreatorResolutionFailure(t *testing.T) {
	store := &provisioningStore{accountErr: errors.New("数据库不可用")}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer upstream.Close()
	h := handler{store: store, cfg: config.ChannelBridgeConfig{URL: upstream.URL, Token: "test-token"}, client: upstream.Client()}
	p := auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a", AccountUUID: "stable-account",
		CredentialType: auth.CredentialTypePlatformSession, OrganizationAccessSource: auth.OrganizationAccessPlatformAdmin,
		WorkspaceAccess: auth.WorkspaceAccess{OrganizationRole: "admin", Role: "workspace_admin", Source: "platform_admin"}}
	r, err := http.NewRequestWithContext(t.Context(), http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.syncWorkspaceCredentials(r, p); err == nil {
		t.Fatal("创建者解析失败时应拒绝创建渠道凭据")
	}
	if store.calls != 0 {
		t.Fatalf("创建者解析失败后仍创建了渠道凭据：%d", store.calls)
	}
}
