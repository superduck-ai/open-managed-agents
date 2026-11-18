package channels

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestChannelCheckUsesRuntimeKeyWithoutExecutingTasks(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "正常", true: "凭据被拒绝"}[fail], func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodGet || r.Header.Get("X-Api-Key") != "key" {
					t.Errorf("预检必须只读并使用渠道凭据: %s", r.Method)
				}
				if fail {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/agents/agent":
					_, _ = w.Write([]byte(`{"id":"agent"}`))
				case "/v1/environments/env":
					_, _ = w.Write([]byte(`{"id":"env"}`))
				default:
					t.Errorf("不应创建会话或执行任务: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			store := &fakeRuntimeStore{env: "env"}
			runtime := testRuntimeSupervisor(store, nil, newOMAClient(server.URL), nil)
			worker := &channelWorker{}
			worker.online.Store(true)
			runtime.workers["channel"] = worker
			got := runtime.check(t.Context(), db.ChannelRecord{ExternalID: "channel", AgentExternalID: "agent", Enabled: true, BindingStatus: "bound"})
			if got.OK == fail || got.ExecutionVerified || calls != 2 {
				t.Fatalf("预检结果错误: %+v calls=%d", got, calls)
			}
		})
	}
}

func TestChannelCheckReportsUnavailableConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, code, environment, unavailable string }{
		{"未配置环境", "environment_binding", "", ""},
		{"环境归档", "environment", "env", "/v1/environments/env"},
		{"Memory归档", "memory", "env", "/v1/memory_stores/memory"},
		{"Vault归档", "vault", "env", "/v1/vaults/vault"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("配置预检不能修改资源: %s", r.Method)
				}
				id := ""
				switch r.URL.Path {
				case "/v1/agents/agent":
					id = "agent"
				case "/v1/environments/env":
					id = "env"
				case "/v1/memory_stores/memory":
					id = "memory"
				case "/v1/vaults/vault":
					id = "vault"
				default:
					t.Errorf("预检访问了无关接口: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				archived := "null"
				if r.URL.Path == tc.unavailable {
					archived = `"2026-01-01T00:00:00Z"`
				}
				_, _ = w.Write([]byte(`{"id":"` + id + `","archived_at":` + archived + `}`))
			}))
			defer server.Close()
			runtime := testRuntimeSupervisor(&fakeRuntimeStore{env: tc.environment}, nil, newOMAClient(server.URL), nil)
			worker := &channelWorker{}
			worker.online.Store(true)
			runtime.workers["channel"] = worker
			result := runtime.check(t.Context(), db.ChannelRecord{ExternalID: "channel", AgentExternalID: "agent", Enabled: true, BindingStatus: "bound", Mounts: `{"memory_store_id":"memory","vault_id":"vault"}`})
			found := false
			for _, item := range result.Checks {
				if item.Code == tc.code {
					found = true
					if item.OK || item.Message == "" {
						t.Fatalf("故障环节没有明确失败说明: %+v", item)
					}
				}
			}
			if result.OK || result.ExecutionVerified || !found {
				t.Fatalf("配置故障被误报为通过: %+v", result)
			}
		})
	}
}
