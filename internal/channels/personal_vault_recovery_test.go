package channels

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestRecoveryUsesCurrentPersonalVault(t *testing.T) {
	for _, state := range []string{"已重建", "读取失败", "当前关联"} {
		for _, command := range []string{"恢复", "历史"} {
			t.Run(state+command, func(t *testing.T) {
				session := omaSession{ID: "sesn_old", Status: "idle", Agent: omaSessionAgent{ID: "agent_1"}, EnvironmentID: "env_1", VaultIDs: []string{"vlt_old"}, Metadata: map[string]string{"source": "feishu", "channel_id": "chnl_1", "app_id": "app_1", "scope_id": "chat:oc_1", "chat_type": "p2p", "actor_id": "user-a", "personal_vault_id": "vlt_old", "credential_binding_mode": "first_vault"}}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if state == "读取失败" {
						t.Error("个人关联读取失败后不应读取历史")
					}
					w.Header().Set("Content-Type", "application/json")
					var body any = session
					if r.URL.Path == "/v1/sessions" {
						body = struct {
							Data []omaSession `json:"data"`
						}{[]omaSession{session}}
					}
					if err := json.MarshalWrite(w, body); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
				lookups := 0
				worker.supervisor.personalVaultLookup = func(_ context.Context, _ db.ChannelRecord, actorID string) (string, bool, error) {
					lookups++
					if actorID != "user-a" {
						t.Errorf("未使用当前消息操作者：%s", actorID)
					}
					if state == "读取失败" {
						return "", false, context.DeadlineExceeded
					}
					if state == "已重建" {
						return "vlt_rebuilt", true, nil
					}
					return "vlt_old", true, nil
				}
				var args []string
				if command == "恢复" {
					args = []string{session.ID}
				}
				worker.resumeCommand(t.Context(), "chat:oc_1", &channelMessage{ChatID: "oc_1", ChatType: "p2p", UserID: "user-a"}, args)
				reply := strings.Join(channel.text(), "\n")
				accepted := strings.Contains(reply, "已恢复 OMA 会话") || strings.Contains(reply, "/resume sesn_old")
				if lookups != 1 || accepted != (state == "当前关联") {
					t.Fatalf("未使用当前个人关联过滤恢复：查询=%d 回复=%s", lookups, reply)
				}
				bound := worker.currentSession(t.Context(), "chat:oc_1")
				if (bound == session.ID) != (command == "恢复" && state == "当前关联") {
					t.Fatalf("失效挂载被重新绑定：%s", bound)
				}
			})
		}
	}
}
