package channels

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestChannelCommandsDoNotBecomeModelInput(t *testing.T) {
	for _, command := range []string{"/help", "/status", "/reset", "/resume"} {
		t.Run(command, func(t *testing.T) {
			server, creates, bodies := omaTestServer(t, 0)
			worker, fake := testWorker(t, &fakeRuntimeStore{}, server.URL)
			if err := worker.dispatchMessage(t.Context(), &channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1", Content: command}); err != nil {
				t.Fatal(err)
			}
			worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
			waitForScope(t, worker, "chat:oc_1")
			worker.mu.Lock()
			tasks := append([]*channelReplyTask{}, worker.replies["chat:oc_1"]...)
			worker.mu.Unlock()
			for _, task := range tasks {
				<-task.done
			}
			if creates.Load() != 0 || len(bodies.snapshot()) != 0 || len(fake.text()) == 0 {
				t.Fatalf("命令必须本地处理，不能创建会话或交给模型: creates=%d bodies=%v replies=%v", creates.Load(), bodies.snapshot(), fake.text())
			}
		})
	}
}

func TestResumeOnlyBindsIdleSessionInCurrentScope(t *testing.T) {
	for _, change := range []string{"兼容", "其他渠道", "其他话题", "其他应用", "其他智能体", "其他环境", "已归档", "运行中", "其他记忆", "其他凭据"} {
		t.Run(change, func(t *testing.T) {
			session := map[string]any{"id": "sesn_old", "status": "idle", "agent": map[string]any{"id": "agent_1", "version": 1}, "environment_id": "env_1", "metadata": map[string]string{"source": "feishu", "channel_id": "chnl_1", "app_id": "app_1", "scope_id": "chat:oc_1"}}
			metadata := session["metadata"].(map[string]string)
			switch change {
			case "其他渠道":
				metadata["channel_id"] = "chnl_other"
			case "其他话题":
				metadata["scope_id"] = "thread:oc_1:omt_other"
			case "其他应用":
				metadata["app_id"] = "app_other"
			case "其他智能体":
				session["agent"] = map[string]any{"id": "agent_other", "version": 1}
			case "其他环境":
				session["environment_id"] = "env_other"
			case "已归档":
				session["archived_at"] = "2026-11-12T00:00:00Z"
			case "运行中":
				session["status"] = "running"
			case "其他记忆":
				session["resources"] = []map[string]string{{"type": "memory_store", "memory_store_id": "mem_other"}}
			case "其他凭据":
				session["vault_ids"] = []string{"vault_other"}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _ = json.MarshalWrite(w, session) }))
			t.Cleanup(server.Close)
			store := &fakeRuntimeStore{}
			worker, fake := testWorker(t, store, server.URL)
			if err := worker.handleMessage(t.Context(), feishuMessage(&channeltypes.NormalizedMessage{MessageID: "om_1", ChatID: "oc_1", UserID: "ou_1", Content: "/resume sesn_old"}), "chat:oc_1"); err != nil {
				t.Fatal(err)
			}
			waitForScope(t, worker, "chat:oc_1")
			if bound := worker.currentSession(t.Context(), "chat:oc_1"); (bound == "sesn_old") != (change == "兼容") {
				t.Fatalf("恢复范围校验失败，绑定=%s 回复=%v", bound, fake.text())
			}
		})
	}
}

func TestResetKeepsBindingWhenRemoteStopIsUnconfirmed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"sesn_1","status":"running"}`))
	}))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, fake := testWorker(t, store, server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	worker.runCommand(ctx, "chat:oc_1", feishuMessage(&channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}), "/new")
	if worker.currentSession(t.Context(), "chat:oc_1") != "sesn_1" || !strings.Contains(strings.Join(fake.text(), ""), "未确认") {
		t.Fatalf("停止未确认不得重置或报告成功: binding=%v replies=%v", store.bindings, fake.text())
	}
}

func TestHistoryPaginatesAndDoesNotLeakOtherScopes(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Query().Get("agent_id") != "agent_1" {
			t.Errorf("历史查询必须带智能体过滤: %s %s", r.Method, r.URL)
		}
		calls++
		scope, id, next := "thread:oc_other:omt_other", "sesn_private", "cursor_2"
		if r.URL.Query().Get("page") == "cursor_2" {
			scope, id, next = "chat:oc_1", "sesn_history", ""
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"` + id + `","status":"idle","agent":{"id":"agent_1","version":3},"environment_id":"env_1","metadata":{"source":"feishu","channel_id":"chnl_1","app_id":"app_1","scope_id":"` + scope + `"}}],"next_page":"` + next + `"}`))
	}))
	t.Cleanup(server.Close)
	worker, fake := testWorker(t, &fakeRuntimeStore{}, server.URL)
	worker.runCommand(t.Context(), "chat:oc_1", feishuMessage(&channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}), "/resume")
	got := strings.Join(fake.text(), "")
	if calls != 2 || !strings.Contains(got, "/resume sesn_history") || strings.Contains(got, "sesn_private") {
		t.Fatalf("分页和话题隔离失败: calls=%d replies=%s", calls, got)
	}
}

func TestResumeKeepsCurrentBindingWhileReplyIsPending(t *testing.T) {
	server, _, _ := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_current"}}
	worker, fake := testWorker(t, store, server.URL)
	task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_current")
	defer worker.finishReply(task)
	worker.runCommand(t.Context(), "chat:oc_1", feishuMessage(&channeltypes.NormalizedMessage{ChatID: "oc_1", UserID: "ou_1"}), "/resume sesn_old")
	if worker.currentSession(t.Context(), "chat:oc_1") != "sesn_current" || !strings.Contains(strings.Join(fake.text(), ""), "未完成") {
		t.Fatalf("回复未结束不得切换会话: %v", fake.text())
	}
}

func TestResumeComparesRepositoryConfiguration(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "")
	worker.record.Mounts = `{"git_repository":{"url":"https://github.com/octocat/Hello-World.git"}}`
	var session omaSession
	if err := json.Unmarshal([]byte(`{"resources":[{"type":"github_repository","url":"https://github.com/octocat/Hello-World.git","mount_path":"/workspace/Hello-World"},{"type":"file","file_id":"file_input"}]}`), &session); err != nil {
		t.Fatal(err)
	}
	if !worker.compatibleMounts(session) {
		t.Fatal("默认挂载路径和用户文件不应阻止恢复")
	}
	session.Resources[0].MountPath = "/workspace/other"
	if worker.compatibleMounts(session) {
		t.Fatal("仓库挂载路径变化时不应恢复旧配置")
	}
}

func TestStopCardChecksActorScopeAndInputTurn(t *testing.T) {
	for _, kind := range []string{"本人当前轮次", "他人", "缺少身份", "其他聊天", "旧轮次", "事件读取失败"} {
		t.Run(kind, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					_, _ = w.Write([]byte(`{}`))
					return
				}
				if kind == "事件读取失败" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				id := "evt_input"
				if kind == "旧轮次" {
					id = "evt_next"
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"` + id + `","type":"user.message"}]}`))
			}))
			t.Cleanup(server.Close)
			worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
			seedCardActionReceipt(worker, "evt_input")
			value := map[string]any{"kind": "stop", "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": "evt_input", "open_id": "ou_1"}
			event := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: value}}
			if kind == "他人" {
				event.Operator.OpenID = "ou_other"
			}
			if kind == "缺少身份" {
				event.Operator.OpenID = ""
			}
			if kind == "其他聊天" {
				event.ChatID = "oc_other"
			}
			err := worker.handleCardAction(t.Context(), event)
			if kind == "本人当前轮次" {
				if err != nil || posts != 1 {
					t.Fatalf("本人当前轮次应发送一次停止: posts=%d err=%v", posts, err)
				}
			} else if err == nil || posts != 0 {
				t.Fatalf("不合法或无法确认的卡片不得停止: posts=%d err=%v", posts, err)
			}
		})
	}
}
