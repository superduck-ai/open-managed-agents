package channels

import (
	"encoding/json/v2"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplyPausesOnForeignInput(t *testing.T) {
	for _, state := range []string{"pending", "processed", "other channel", "other account", "other chat"} {
		t.Run(state, func(t *testing.T) {
			worker, channel := testWorker(t, &fakeRuntimeStore{}, "http://unused")
			task := &channelReplyTask{scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "own"}
			runner := newReplyRunner(worker, task, &channelMessage{ChatID: "oc_1", UserID: "ou_1"})
			runner.stream = fakeStream{channel: channel}
			input := omaEvent{ID: "foreign", Type: "user.message", ProcessedAt: []byte(`null`)}
			metadata := channelInputMetadata{Type: "feishu", ChannelID: "chnl_1", AccountID: "app_1", ChatID: "oc_1", MessageID: "foreign_message", ActorID: "ou_1"}
			switch state {
			case "processed":
				input.ProcessedAt = []byte(`"2026-11-13T05:00:00Z"`)
			case "other channel":
				metadata.ChannelID = "chnl_other"
			case "other account":
				metadata.AccountID = "app_other"
			case "other chat":
				metadata.ChatID = "oc_other"
			}
			if strings.HasPrefix(state, "other") {
				input.Metadata = metadataJSON(t, metadata)
			}
			done, err := runner.consumeEvent(t.Context(), input)
			if err != nil || !done || !task.terminal || task.normalEnd || runner.reply.status != "已暂停" || !strings.Contains(runner.reply.text.String(), "外部输入") {
				t.Fatalf("外部输入应暂停渠道交付，不能按正常分轮完成: done=%v err=%v terminal=%v normal=%v status=%q text=%q", done, err, task.terminal, task.normalEnd, runner.reply.status, runner.reply.text.String())
			}
		})
	}
}

func TestChannelInputOwnershipHistoryBoundary(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	own := omaEvent{ID: "own", Type: "user.message", Metadata: metadataJSON(t, channelInputMetadata{Type: "feishu", ChannelID: "chnl_1", AccountID: "app_1", ChatID: "oc_1", MessageID: "message", ActorID: "another_actor"})}
	legacy := omaEvent{ID: "legacy", Type: "user.message"}
	foreign := omaEvent{ID: "foreign", Type: "user.message", Metadata: []byte(`{"source":"console"}`)}
	for _, test := range []struct {
		name     string
		events   []omaEvent
		conflict bool
	}{
		{"新会话", nil, false},
		{"迁移前未标记历史", []omaEvent{legacy, own}, false},
		{"同渠道其他操作者", []omaEvent{own, own}, false},
		{"接管后无标记输入", []omaEvent{own, legacy}, true},
		{"接管前明确外部来源", []omaEvent{foreign, own}, true},
		{"接管后明确外部来源", []omaEvent{own, foreign}, true},
		{"控制台审批", []omaEvent{own, {ID: "approval", Type: "user.tool_confirmation"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := channelHistoryHasForeignInput(worker.record, "oc_1", test.events, false); got != test.conflict {
				t.Fatalf("输入历史归属不符: got=%v want=%v", got, test.conflict)
			}
		})
	}
}

func TestForeignInputBlocksSubmissionBeforeAttachments(t *testing.T) {
	requests := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/events") {
			t.Error("归属冲突时不应准备附件或提交新输入")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		own := channelInputMetadata{Type: "feishu", ChannelID: "chnl_1", AccountID: "app_1", ChatID: "oc_1", MessageID: "earlier_message", ActorID: "ou_1"}
		if err := json.MarshalWrite(w, struct {
			Data []omaEvent `json:"data"`
		}{[]omaEvent{{ID: "own", Type: "user.message", Metadata: metadataJSON(t, own)}, {ID: "foreign", Type: "user.message"}}}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	worker, channel := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
	task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_1")
	worker.deliver(task, &channelMessage{ChatID: "oc_1", UserID: "ou_1", MessageID: "new_message", Content: "不能混入原会话"}, "key")
	if len(requests) != 1 || len(channel.text()) != 1 || channel.text()[0] != channelForeignInputMessage {
		t.Fatalf("冲突时只能读取原历史并提示，不能提交: requests=%d replies=%v", len(requests), channel.text())
	}
}

func TestInputHistoryReusesNativeEventCursorAndChecksForeignInput(t *testing.T) {
	own := channelInputMetadata{Type: "feishu", ChannelID: "chnl_1", AccountID: "app_1", ChatID: "oc_1", MessageID: "message", ActorID: "actor"}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("types[]") != "user.message" {
			t.Error("只能读取原生用户输入")
		}
		after := r.URL.Query().Get("page")
		events := []omaEvent{{ID: "own", Type: "user.message", ProcessedAt: []byte(`"2026-11-14T00:00:00Z"`), Metadata: metadataJSON(t, own)}}
		if calls == 2 {
			if after != sessions.EncodeEventCursor("own", "asc") {
				t.Errorf("未复用原生事件游标: %q", after)
			}
			events = []omaEvent{{ID: "foreign", Type: "user.message"}}
		} else if after != "" {
			t.Errorf("首次检查或重启后应重新补读: %q", after)
		}
		_ = json.MarshalWrite(w, struct {
			Data []omaEvent `json:"data"`
		}{events})
	}))
	defer server.Close()
	worker, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	if conflict, err := worker.checkInputHistory(t.Context(), "key", "session", "oc_1"); err != nil || conflict {
		t.Fatalf("首次归属检查: %v %v", conflict, err)
	}
	if conflict, err := worker.checkInputHistory(t.Context(), "key", "session", "oc_1"); err != nil || !conflict {
		t.Fatalf("增量外部输入必须阻断: %v %v", conflict, err)
	}
	restarted, _ := testWorker(t, &fakeRuntimeStore{}, server.URL)
	if conflict, err := restarted.checkInputHistory(t.Context(), "key", "session", "oc_1"); err != nil || conflict {
		t.Fatalf("重启后原生补读: %v %v", conflict, err)
	}
}
