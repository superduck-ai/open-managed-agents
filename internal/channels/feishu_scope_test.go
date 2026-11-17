package channels

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestGroupRootAndRepliesUseSameScope(t *testing.T) {
	root := &channeltypes.NormalizedMessage{ChatID: "group", ChatType: "group", MessageID: "om_root"}
	reply := &channeltypes.NormalizedMessage{ChatID: "group", ChatType: "group", MessageID: "om_reply", RawEvent: map[string]any{"root_id": "om_root", "thread_id": "omt_thread"}}
	other := &channeltypes.NormalizedMessage{ChatID: "group", ChatType: "group", MessageID: "om_other"}
	want := "thread:group:om_root"
	if channelScopeKey(feishuMessage(root)) != want || channelScopeKey(feishuMessage(reply)) != want {
		t.Fatalf("根消息与回复必须共用根消息作用域: root=%s reply=%s", channelScopeKey(feishuMessage(root)), channelScopeKey(feishuMessage(reply)))
	}
	if channelScopeKey(feishuMessage(other)) == want {
		t.Fatal("同群不同根消息不能共享作用域")
	}
}

func TestMentionedGroupCommandDoesNotReachModel(t *testing.T) {
	server, creates, recorder := omaTestServer(t, 0)
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	defer worker.stopAllReplies()
	msg := &channeltypes.NormalizedMessage{ChatID: "group", ChatType: "group", MessageID: "om_command", UserID: "user", Content: "@_user_1 /help", Mentions: []channeltypes.Mention{{Key: "@_user_1", IsBot: true}}}
	if err := worker.dispatchMessage(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
	deadline := time.Now().Add(time.Second)
	for len(channel.text()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if creates.Load() != 0 || len(recorder.snapshot()) != 0 || len(channel.text()) != 1 {
		t.Fatalf("群聊 @ 前缀不能把命令交给模型: creates=%d events=%v replies=%v", creates.Load(), recorder.snapshot(), channel.text())
	}
}

func TestGroupScopeReadsRealSDKRootBeforeThread(t *testing.T) {
	var raw larkim.P2MessageReceiveV1
	if err := json.Unmarshal([]byte(`{"schema":"2.0","event":{"message":{"message_id":"om_reply","chat_id":"group","chat_type":"group","root_id":"om_root","thread_id":"omt_thread"}}}`), &raw); err != nil {
		t.Fatal(err)
	}
	msg := normalize.ParseMessage(&raw)
	if got := channelScopeKey(feishuMessage(msg)); got != "thread:group:om_root" {
		t.Fatalf("SDK 接收边界应优先使用根消息: %s", got)
	}
}

func TestGroupScopeKeepsExistingThreadBinding(t *testing.T) {
	for _, kind := range []string{"未绑定", "已有旧话题", "已有根绑定", "关闭话题回复", "私聊"} {
		t.Run(kind, func(t *testing.T) {
			store := &fakeRuntimeStore{bindings: map[string]string{}}
			worker, _ := testWorker(t, store, "")
			worker.record.ReplyInThread = true
			msg := &channeltypes.NormalizedMessage{ChatID: "group", ChatType: "group", MessageID: "om_reply", RawEvent: map[string]any{"root_id": "om_root", "thread_id": "omt_thread"}}
			want := "thread:group:om_root"
			if kind == "已有旧话题" || kind == "已有根绑定" {
				store.bindings["thread:group:omt_thread"] = "sesn_old"
				want = "thread:group:omt_thread"
			}
			if kind == "已有根绑定" {
				store.bindings["thread:group:om_root"] = "sesn_root"
				want = "thread:group:om_root"
			}
			if kind == "关闭话题回复" {
				worker.record.ReplyInThread = false
				want = "chat:group"
			}
			if kind == "私聊" {
				msg.ChatType = "p2p"
				want = "chat:group"
			}
			got, err := worker.messageScopeKey(t.Context(), feishuMessage(msg))
			if err != nil || got != want {
				t.Fatalf("scope=%s want=%s err=%v", got, want, err)
			}
		})
	}
}

func TestGroupHistoryAcceptsOnlyCurrentRootOrLegacyThread(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "")
	for _, scope := range []string{"thread:group:om_root", "thread:group:omt_thread", "thread:group:omt_other", "chat:group", ""} {
		session := omaSession{ID: "sesn_old", Status: "idle", Agent: omaSessionAgent{ID: "agent_1"}, EnvironmentID: "env_1", Metadata: map[string]string{"source": "feishu", "channel_id": "chnl_1", "app_id": "app_1", "scope_id": scope}}
		allowed := scope == "thread:group:om_root" || scope == "thread:group:omt_thread"
		if worker.compatibleSession(session, "thread:group:om_root", "thread:group:omt_thread", "env_1", "") != allowed {
			t.Fatalf("历史恢复作用域错误: %s", scope)
		}
	}
}
