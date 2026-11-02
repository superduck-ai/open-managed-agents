package channels

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platform"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type provisioningStore struct {
	scopedStore
	input   platform.CreateConsoleAPIKeyInput
	failure error
	calls   int
}

func (s *provisioningStore) EnsureChannelWorkspaceCredential(_ context.Context, input platform.CreateConsoleAPIKeyInput, _ *secrets.Service) (string, error) {
	s.calls++
	s.input = input
	return "sk-ant-api03-workspace-only", s.failure
}

func TestChannelProvisioning(t *testing.T) {
	for _, test := range []struct {
		name             string
		credentialError  error
		syncStatus, want int
	}{
		{"凭据停用不创建新渠道", db.ErrChannelCredentialInactive, 204, 409},
		{"凭据持久化失败不访问桥接", errors.New("storage failed"), 204, 503},
		{"桥接版本不支持同步时阻止创建", nil, 404, 503},
		{"同步失败不创建渠道", nil, 503, 503},
		{"同步成功后才创建渠道", nil, 204, 201},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &provisioningStore{failure: test.credentialError}
			var paths []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.Header.Get("Cookie") != "" || r.Header.Get("X-OMA-Organization") != "organization-a" || r.Header.Get("X-OMA-Workspace") != "workspace-a" {
					t.Error("同步未隔离浏览器凭据和工作区")
				}
				switch r.URL.Path {
				case "/api/managed-channels/settings":
					_, _ = io.WriteString(w, `{"environment_id":"env-a"}`)
				case "/api/managed-channels/runtime":
					body, _ := io.ReadAll(r.Body)
					if string(body) != `{"api_key":"sk-ant-api03-workspace-only"}` {
						t.Error("同步载荷不正确")
					}
					w.WriteHeader(test.syncStatus)
				default:
					body, _ := io.ReadAll(r.Body)
					if string(body) != `{"name":"机器人"}` {
						t.Error("原渠道请求被同步请求修改")
					}
					w.WriteHeader(201)
					_, _ = io.WriteString(w, `{"id":"channel-a"}`)
				}
			}))
			defer upstream.Close()
			router := chi.NewRouter()
			router.Mount("/agents/{agent_id}/channels", NewHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))
			request := httptest.NewRequest(http.MethodPost, "/agents/agent-a/channels", strings.NewReader(`{"name":"机器人"}`))
			request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a", UserUUID: "user-a"}))
			request.Header.Set("Cookie", "private")
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			if result.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", result.Code, test.want, result.Body.String())
			}
			if store.calls != 1 || store.input.OrgUUID != "organization-a" || store.input.WorkspaceUUID != "workspace-a" || *store.input.CreatedByUserUUID != "user-a" {
				t.Fatal("身份创建未使用已认证范围")
			}
			if test.want == 201 && strings.Join(paths, ",") != "/api/managed-channels/settings,/api/managed-channels/runtime,/api/managed-channels" {
				t.Fatalf("调用顺序错误: %v", paths)
			}
			if test.want != 201 && paths[len(paths)-1] == "/api/managed-channels" {
				t.Fatal("失败后仍然创建渠道")
			}
			if strings.Contains(result.Body.String(), "sk-ant-api03") {
				t.Fatal("凭据泄露到浏览器")
			}
		})
	}
}
