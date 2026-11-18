package channels

import (
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestDirectConversationIgnoresThreadAndKeepsLegacyHistory(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "")
	worker.record.ReplyInThread = true
	for _, thread := range []string{"", "old_a", "old_b"} {
		scope, err := worker.messageScopeKey(t.Context(), feishuMessage(&channeltypes.NormalizedMessage{ChatID: "direct", ChatType: "p2p", MessageID: "message", RawEvent: map[string]any{"thread_id": thread, "root_id": "root"}}))
		if err != nil || scope != "chat:direct" {
			t.Fatalf("私聊不能按话题分割: %s %v", scope, err)
		}
	}
	for _, tc := range []struct {
		kind, scope string
		allowed     bool
	}{
		{"p2p", "chat:direct", true},
		{"p2p", "thread:direct:old_a", true},
		{"p2p", "thread:other:old_a", false},
		{"group", "thread:direct:old_a", false},
	} {
		session := omaSession{ID: "session", Status: "idle", Agent: omaSessionAgent{ID: "agent_1"}, EnvironmentID: "env_1", Metadata: map[string]string{"source": "feishu", "channel_id": "chnl_1", "app_id": "app_1", "chat_type": tc.kind, "scope_id": tc.scope}}
		if got := worker.compatibleSession(session, "chat:direct", "", "env_1", ""); got != tc.allowed {
			t.Fatalf("旧私聊会话恢复边界错误: %+v got=%v", tc, got)
		}
	}
}

func TestDirectRepliesRemainFlatWhenGroupThreadsAreEnabled(t *testing.T) {
	worker, channel := testWorker(t, &fakeRuntimeStore{}, "")
	worker.record.ReplyInThread = true
	message := &channelMessage{ChatID: "direct", ChatType: "p2p", UserID: "user", MessageID: "old_anchor", ThreadID: "old_thread"}
	stream, err := worker.adapter.OpenReply(t.Context(), message)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Update(t.Context(), (&channelReply{}).view()); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := worker.replyText(t.Context(), message, "命令确认"); err != nil {
		t.Fatal(err)
	}
	attachFeishuStub(t, worker)
	if _, err := worker.adapter.SendMedia(t.Context(), message, omaResource{Filename: "result.txt", MimeType: "text/plain"}, "file", []byte("result")); err != nil {
		t.Fatal(err)
	}
	inputs := channel.sentInputs()
	if len(inputs) != 3 || inputs[0].Card == "" || inputs[1].Text != "命令确认" || inputs[2].FileKey == "" {
		t.Fatalf("私聊必须直接发卡片和文字: %+v", inputs)
	}
}
