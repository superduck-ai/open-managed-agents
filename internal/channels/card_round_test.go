package channels

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
)

func TestCardApprovalUsesCurrentInputAndPersistedThread(t *testing.T) {
	for _, kind := range []string{"当前工具", "排队输入", "前轮工具", "后轮已开始", "已结束", "无需审批"} {
		t.Run(kind, func(t *testing.T) {
			events := []omaEvent{{ID: "card_input", Type: "user.message"}, {ID: "target", Type: "agent.tool_use", Permission: "ask", Name: "bash", SessionThreadID: "actual_thread"}}
			switch kind {
			case "前轮工具":
				events[0], events[1] = events[1], events[0]
			case "排队输入":
				events = append(events, omaEvent{ID: "future", Type: "user.message", ProcessedAt: []byte(`null`)})
			case "后轮已开始":
				events = append(events, omaEvent{ID: "next_input", Type: "user.message", ProcessedAt: []byte(`"2026-11-13T00:00:00Z"`)})
			case "已结束":
				events = append(events, omaEvent{ID: "done", Type: "session.status_idle", StopReason: []byte(`"end_turn"`)})
			case "无需审批":
				events[1].Permission = "allow"
			}
			bodies := &omaRecorder{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					body, _ := io.ReadAll(r.Body)
					bodies.record(string(body))
					_, _ = io.WriteString(w, `{}`)
					return
				}
				payload, err := json.Marshal(map[string]any{"data": events})
				if err != nil {
					t.Error(err)
					return
				}
				if kind == "排队输入" {
					payload = bytes.Replace(payload, []byte(`"id":"future"`), []byte(`"id":"future","processed_at":null`), 1)
				}
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			worker, _ := testWorker(t, &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}, server.URL)
			seedCardActionReceipt(worker, "card_input")
			task := worker.startReplyTask(t.Context(), "chat:oc_1", "sesn_1")
			defer worker.finishReply(task)
			event := &channeltypes.CardActionEvent{ChatID: "oc_1", MessageID: "om_card", Operator: channeltypes.CardActionOperator{OpenID: "ou_1"}, Action: channeltypes.CardActionPayload{Value: map[string]any{"kind": "confirm", "result": "allow", "channel_id": "chnl_1", "scope_key": "chat:oc_1", "session_id": "sesn_1", "tool_use_id": "target", "open_id": "ou_1", "session_thread_id": "forged_thread"}}}
			err := worker.handleCardAction(t.Context(), event)
			sent := bodies.snapshot()
			if kind == "当前工具" || kind == "排队输入" {
				if err != nil || len(sent) != 1 || !strings.Contains(sent[0], `"session_thread_id":"actual_thread"`) || strings.Contains(sent[0], "forged_thread") {
					t.Fatalf("只能批准本轮实际工具及线程: kind=%s err=%v sent=%v", kind, err, sent)
				}
			} else if err == nil || len(sent) != 0 {
				t.Fatalf("跨轮或非待审批工具不能提交: kind=%s err=%v sent=%v", kind, err, sent)
			}
		})
	}
}
