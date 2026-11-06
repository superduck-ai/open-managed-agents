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
	input         platform.CreateConsoleAPIKeyInput
	failure       error
	accountErr    error
	memberErr     error
	creatorMember string
	calls         int
}

func (s *provisioningStore) EnsureChannelWorkspaceCredential(_ context.Context, input platform.CreateConsoleAPIKeyInput, _ *secrets.Service) (string, error) {
	s.calls++
	s.input = input
	return "sk-ant-api03-workspace-only", s.failure
}

func (s *provisioningStore) GetPlatformAdminAccount(_ context.Context, accountUUID string) (db.PlatformAdminAccount, error) {
	if s.accountErr != nil {
		return db.PlatformAdminAccount{}, s.accountErr
	}
	if accountUUID != "stable-account" {
		return db.PlatformAdminAccount{}, db.ErrNotFound
	}
	return db.PlatformAdminAccount{UUID: accountUUID, Email: "actor@example.com"}, nil
}

func (s *provisioningStore) GetActivePlatformUserByEmail(_ context.Context, organizationUUID, email string) (db.AdminUser, error) {
	if s.memberErr != nil {
		return db.AdminUser{}, s.memberErr
	}
	if organizationUUID != "organization-a" || email != "actor@example.com" || s.creatorMember == "" {
		return db.AdminUser{}, db.ErrNotFound
	}
	return db.AdminUser{UUID: s.creatorMember}, nil
}

func TestChannelProvisioning(t *testing.T) {
	for _, test := range []struct {
		name             string
		credentialError  error
		syncStatus, want int
	}{
		{"凭据停用不创建新渠道", db.ErrChannelCredentialInactive, 204, 409},
		{"密文不可读时明确报错", db.ErrChannelCredentialUnreadable, 204, 503},
		{"工作区失效返回不存在", db.ErrNotFound, 204, 404},
		{"凭据持久化失败不访问桥接", errors.New("storage failed"), 204, 503},
		{"桥接版本不支持同步时阻止创建", nil, 404, 503},
		{"同步失败不创建渠道", nil, 503, 503},
		{"同步成功后才创建渠道", nil, 204, 201},
	} {
		for _, suffix := range []string{"", "/channel-a"} {
			t.Run(test.name+suffix, func(t *testing.T) {
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
					case "/api/managed-channels/internal/workspace-runtime":
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
				request := httptest.NewRequest(http.MethodPost, "/agents/agent-a/channels"+suffix, strings.NewReader(`{"name":"机器人"}`))
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
				if test.want == 201 && strings.Join(paths, ",") != "/api/managed-channels/settings,/api/managed-channels/internal/workspace-runtime,/api/managed-channels"+suffix {
					t.Fatalf("调用顺序错误: %v", paths)
				}
				if test.want != 201 && paths[len(paths)-1] == "/api/managed-channels"+suffix {
					t.Fatal("失败后仍然创建渠道")
				}
				if strings.Contains(result.Body.String(), "sk-ant-api03") {
					t.Fatal("凭据泄露到浏览器")
				}
			})
		}
	}
}

func TestChannelRuntimeSyncIsNotPublic(t *testing.T) {
	store := &provisioningStore{}
	router := chi.NewRouter()
	router.Mount("/agents/{agent_id}/channels", NewHandler(config.ChannelBridgeConfig{URL: "http://127.0.0.1:1", Token: "bridge-token"}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))
	request := httptest.NewRequest(http.MethodPost, "/agents/agent-a/channels/internal/workspace-runtime", strings.NewReader(`{"api_key":"browser-supplied"}`))
	request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a"}))
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	if result.Code != http.StatusNotFound || store.calls != 0 {
		t.Fatal("浏览器可以访问内部凭据登记路由")
	}
}

func TestChannelStopDoesNotRequireCredentials(t *testing.T) {
	for _, test := range []struct {
		name, suffix, body, environment string
		want, keyCalls                  int
	}{
		{"拒绝无效 JSON", "/channel-a", `{`, `"env-a"`, 400, 0},
		{"启用仍需有效凭据", "/channel-a", `{"enabled":true}`, `"env-a"`, 409, 1},
		{"混合更新不能绕过凭据", "/channel-a", `{"enabled":false,"name":"修改"}`, `"env-a"`, 409, 1},
		{"创建仍需有效凭据", "", `{"enabled":false}`, `"env-a"`, 409, 1},
		{"绑定仍需有效凭据", "/channel-a/qr", `{"enabled":false}`, `"env-a"`, 409, 1},
		{"未选择环境仍可纯停用", "/channel-a", `{"enabled":false}`, `null`, 200, 0},
		{"已归档环境仍可纯停用", "/channel-a", `{"enabled":false}`, `"env-archived"`, 200, 0},
		{"配对审批使用既有身份", "/channel-a/pairings/pair-a", `{"status":"approved"}`, `null`, 200, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &provisioningStore{failure: db.ErrChannelCredentialInactive}
			var paths []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/api/managed-channels/settings" {
					_, _ = io.WriteString(w, `{"environment_id":`+test.environment+`}`)
					return
				}
				if r.URL.Path != "/api/managed-channels"+test.suffix {
					t.Error("请求路径发生变化")
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != test.body {
					t.Error("请求正文被校验消耗或改变")
				}
				_, _ = io.WriteString(w, `{"enabled":false}`)
			}))
			defer upstream.Close()
			router := chi.NewRouter()
			router.Mount("/agents/{agent_id}/channels", NewHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), nil))
			request := httptest.NewRequest(http.MethodPost, "/agents/agent-a/channels"+test.suffix, strings.NewReader(test.body))
			request = request.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a"}))
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			if result.Code != test.want || store.calls != test.keyCalls {
				t.Fatalf("状态=%d，签发次数=%d，响应=%s", result.Code, store.calls, result.Body.String())
			}
			if test.want == 200 && (len(paths) != 1 || paths[0] != "/api/managed-channels"+test.suffix) {
				t.Fatalf("不应读取环境或登记凭据: %v", paths)
			}
		})
	}
}
