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

func TestChannelEnvironmentSettings(t *testing.T) {
	for _, test := range []struct {
		name, body string
		want       int
	}{
		{"拒绝跨工作区环境", `{"environment_id":"env-other"}`, 400},
		{"拒绝已归档环境", `{"environment_id":"env-archived"}`, 400},
		{"拒绝空对象", `null`, 400},
		{"拒绝无效类型", `{"environment_id":123}`, 400},
		{"拒绝任意运行凭据", `{"environment_id":"env-a","api_key":"secret"}`, 400},
		{"保存当前工作区环境", `{"environment_id":"env-a"}`, 200},
		{"恢复预设环境", `{"environment_id":null}`, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/managed-channels/settings" {
					t.Errorf("path = %s", r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != test.body {
					t.Errorf("body = %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			}))
			defer upstream.Close()
			router := chi.NewRouter()
			router.Mount("/agents/{agent_id}/channels", NewHandler(config.ChannelBridgeConfig{URL: upstream.URL, Token: "bridge-token"}, scopedStore{}, slog.New(slog.NewTextHandler(io.Discard, nil))))
			req := httptest.NewRequest(http.MethodPost, "/agents/agent-a/channels/settings", strings.NewReader(test.body))
			req = req.WithContext(auth.WithPrincipal(t.Context(), auth.Principal{OrganizationUUID: "organization-a", WorkspaceUUID: "workspace-a"}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
			if (test.want == 200) != (calls == 1) {
				t.Errorf("upstream calls = %d", calls)
			}
		})
	}
}
