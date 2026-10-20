package channels

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type scopedStore struct{ archived bool }

func (s scopedStore) GetAgent(_ context.Context, workspace, id string) (db.Agent, error) {
	if workspace != "workspace-a" || id != "agent-a" {
		return db.Agent{}, db.ErrNotFound
	}
	agent := db.Agent{}
	if s.archived {
		agent.ArchivedAt = new(time.Now())
	}
	return agent, nil
}

func TestChannelScopeAndProxy(t *testing.T) {
	for _, test := range []struct {
		name, workspace, method string
		suffix                  string
		archived                bool
		want                    int
	}{
		{name: "未认证", method: "GET", want: 401},
		{name: "跨工作区拒绝", workspace: "workspace-b", method: "GET", want: 404},
		{name: "归档后禁止写入", workspace: "workspace-a", method: "POST", archived: true, want: 409},
		{name: "工作区内读取", workspace: "workspace-a", method: "GET", want: 200},
		{name: "凭据绑定转发", workspace: "workspace-a", method: "POST", want: 200},
		{name: "生成二维码路径", workspace: "workspace-a", method: "POST", suffix: "/ch1/qr", want: 200},
		{name: "读取二维码路径", workspace: "workspace-a", method: "GET", suffix: "/ch1/qr", want: 200},
		{name: "读取配对路径", workspace: "workspace-a", method: "GET", suffix: "/ch1/pairings", want: 200},
		{name: "批准配对路径", workspace: "workspace-a", method: "POST", suffix: "/ch1/pairings/pair-9", want: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer bridge-token" || r.Header.Get("X-OMA-Workspace") != "workspace-a" || r.Header.Get("X-OMA-Organization") != "organization-a" || r.Header.Get("X-OMA-Agent") != "agent-a" {
					t.Error("桥接范围或身份错误")
				}
				if r.Header.Get("Cookie") != "" || r.URL.RawQuery != "" {
					t.Error("不应向桥接服务传递浏览器凭据或查询参数")
				}
				if r.URL.Path != "/api/managed-channels"+test.suffix {
					t.Errorf("path = %s", r.URL.Path)
				}
				if r.Method != test.method {
					t.Errorf("method = %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer upstream.Close()
			router := chi.NewRouter()
			router.Mount("/agents/{agent_id}/channels", NewHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, scopedStore{archived: test.archived}, slog.New(slog.NewTextHandler(io.Discard, nil))))
			request := httptest.NewRequest(test.method, "/agents/agent-a/channels"+test.suffix+"?workspace=evil", strings.NewReader(`{"name":"飞书"}`))
			request.Header.Set("Cookie", "session=private")
			request.Header.Set("X-OMA-Workspace", "evil")
			if test.workspace != "" {
				request = request.WithContext(auth.WithPrincipal(request.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: test.workspace}))
			}
			result := httptest.NewRecorder()
			router.ServeHTTP(result, request)
			if result.Code != test.want {
				t.Fatalf("status = %d: %s", result.Code, result.Body.String())
			}
			if test.want != 200 && calls != 0 {
				t.Fatal("未授权请求到达桥接服务")
			}
		})
	}
}
