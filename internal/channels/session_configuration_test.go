package channels

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConversationBindingFollowsExecutionConfiguration(t *testing.T) {
	for _, change := range []string{"不变", "环境", "Memory", "Vault", "Git"} {
		t.Run(change, func(t *testing.T) {
			created := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost && r.URL.Path == "/v1/sessions" {
					created++
					var body struct {
						EnvironmentID string `json:"environment_id"`
					}
					if err := json.UnmarshalRead(r.Body, &body); err != nil {
						t.Error(err)
					}
					expected := "env_1"
					if change == "环境" {
						expected = "env_new"
					}
					if body.EnvironmentID != expected {
						t.Errorf("新会话没有使用当前环境: %s", body.EnvironmentID)
					}
					_, _ = w.Write([]byte(`{"id":"sesn_new"}`))
					return
				}
				if r.Method != http.MethodGet || r.URL.Path != "/v1/sessions/sesn_old" {
					t.Errorf("不应修改旧会话: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(`{"id":"sesn_old","agent":{"id":"agent_1","version":1},"environment_id":"env_1","status":"idle"}`))
			}))
			defer server.Close()
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_old"}}
			worker, _ := testWorker(t, store, server.URL)
			switch change {
			case "环境":
				store.env = "env_new"
			case "Memory":
				worker.record.Mounts = `{"memory_store_id":"memory_new"}`
			case "Vault":
				worker.record.Mounts = `{"vault_id":"vault_new"}`
			case "Git":
				worker.record.Mounts = `{"git_repository":{"url":"https://github.com/octocat/Hello-World.git"}}`
			}
			session, err := worker.sessionFor(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1", Content: "继续"}, "chat:oc_1")
			if err != nil {
				t.Fatal(err)
			}
			expected := "sesn_new"
			count := 1
			if change == "不变" {
				expected = "sesn_old"
				count = 0
			}
			if session != expected || created != count {
				t.Fatalf("配置变化没有替换绑定或相同配置被重建: session=%s created=%d", session, created)
			}
		})
	}
}
