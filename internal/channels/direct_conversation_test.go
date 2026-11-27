package channels

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
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

func TestDirectTopicsRouteRepliesToTheirOriginalSessions(t *testing.T) {
	var created atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/v1/sessions" {
			_, _ = fmt.Fprintf(w, `{"id":"sesn_%d"}`, created.Add(1))
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"idle","agent":{"id":"agent_1"},"environment_id":"env_1"}`)
	}))
	t.Cleanup(server.Close)
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	worker.record.DirectInThread = true
	for _, turn := range []struct {
		message, root, thread, text, scope, session string
	}{
		{"om_help", "", "", "/help", "chat:oc_test", ""},
		{"om_a", "", "", "任务 A", "thread:oc_test:om_a", "sesn_1"},
		{"om_b", "om_a", "", "引用后提交任务 B", "thread:oc_test:om_b", "sesn_2"},
		{"om_a1", "om_a", "omt_a", "追问 A", "thread:oc_test:om_a", "sesn_1"},
		{"om_b1", "om_b", "omt_b", "追问 B", "thread:oc_test:om_b", "sesn_2"},
		{"om_a2", "om_a", "omt_a", "/status", "thread:oc_test:om_a", ""},
	} {
		var event larkim.P2MessageReceiveV1
		raw := fmt.Sprintf(`{"event":{"message":{"message_id":%q,"root_id":%q,"thread_id":%q,"chat_id":"oc_test","chat_type":"p2p"}}}`, turn.message, turn.root, turn.thread)
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			t.Fatal(err)
		}
		msg := feishuMessage(normalize.ParseMessage(&event))
		msg.Content, msg.UserID = turn.text, "ou_test"
		scope, err := worker.messageScopeKey(t.Context(), msg)
		if err != nil || scope != turn.scope {
			t.Fatalf("%s 的作用域 = %q，错误 = %v", turn.message, scope, err)
		}
		if turn.session != "" {
			session, err := worker.sessionFor(t.Context(), msg, scope)
			if err != nil || session != turn.session {
				t.Fatalf("%s 的会话 = %q，错误 = %v", turn.message, session, err)
			}
		}
	}
}
