package channels

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestWorkspaceAuthorizations(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-OMA-Organization") != "org-a" || r.Header.Get("X-OMA-Workspace") != "workspace-a" || r.Header.Get("X-OMA-Agent") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "Bearer bridge-token" {
			t.Error("授权转发范围或身份不正确")
		}
		if r.URL.Path != "/api/managed-channels/authorizations" && r.URL.Path != "/api/managed-channels/authorizations/"+id {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"`+id+`","status":"pending","url":"https://open.feishu.cn/page/launcher"}`)
	}))
	defer upstream.Close()
	router := chi.NewRouter()
	router.Mount("/channels", NewWorkspaceHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, scopedStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))
	for _, tc := range []struct {
		name, method, path string
		principal          auth.Principal
		status             int
	}{
		{"未登录", "POST", "/channels/authorizations", auth.Principal{}, 401},
		{"浏览器缺少CSRF", "POST", "/channels/authorizations", auth.Principal{OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a", CredentialType: auth.CredentialTypePlatformSession}, 403},
		{"无效编号", "GET", "/channels/authorizations/invalid", auth.Principal{OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a"}, 400},
		{"不依赖智能体或环境发起授权", "POST", "/channels/authorizations", auth.Principal{OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a"}, 200},
		{"读取工作区授权", "GET", "/channels/authorizations/" + id, auth.Principal{OrganizationUUID: "org-a", WorkspaceUUID: "workspace-a"}, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"name":"测试渠道"}`))
			request = request.WithContext(auth.WithPrincipal(request.Context(), tc.principal))
			request.Header.Set("X-OMA-Agent", "untrusted-agent")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
	if calls != 2 {
		t.Fatalf("上游调用次数 = %d", calls)
	}
}
