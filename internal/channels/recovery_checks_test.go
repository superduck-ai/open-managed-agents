package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/identities"
)

type unreadableBindingStore struct{ *fakeRuntimeStore }

func (s *unreadableBindingStore) GetChannelSessionBinding(context.Context, string, string) (string, error) {
	return "", context.DeadlineExceeded
}

func TestSessionLookupFailurePreservesBinding(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			created := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					created++
					_, _ = w.Write([]byte(`{"id":"sesn_unexpected"}`))
					return
				}
				http.Error(w, "读取失败", status)
			}))
			defer server.Close()
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_original"}}
			worker, _ := testWorker(t, store, server.URL)
			id, err := worker.sessionFor(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1"}, "chat:oc_1")
			if err == nil || id != "" || created != 0 || store.binding("chat:oc_1") != "sesn_original" {
				t.Fatalf("旧会话读取失败后不应创建或改绑: id=%s err=%v created=%d binding=%s", id, err, created, store.binding("chat:oc_1"))
			}
			msg := &channelMessage{ChatID: "oc_1", UserID: "ou_1", Content: "继续原任务"}
			worker.runScoped(t.Context(), worker.enqueueScope("chat:oc_1"), msg, msg.Content)
			if created != 0 || store.binding("chat:oc_1") != "sesn_original" {
				t.Fatal("消息入口在状态未知时创建了新会话")
			}
		})
	}
}

func TestBindingReadFailureDoesNotCreateSession(t *testing.T) {
	created := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		created++
		_, _ = w.Write([]byte(`{"id":"sesn_unexpected"}`))
	}))
	defer server.Close()
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_original"}}
	worker, _ := testWorker(t, store, server.URL)
	worker.supervisor.store = &unreadableBindingStore{store}
	id, err := worker.sessionFor(t.Context(), &channelMessage{ChatID: "oc_1", UserID: "ou_1"}, "chat:oc_1")
	if !errors.Is(err, context.DeadlineExceeded) || id != "" || created != 0 || store.binding("chat:oc_1") != "sesn_original" {
		t.Fatalf("绑定读取异常被误当成无绑定: id=%s err=%v requests=%d", id, err, created)
	}
	msg := &channelMessage{Input: &db.ChannelInput{SessionExternalID: "sesn_original"}}
	if _, err := worker.inputSession(t.Context(), msg, "chat:oc_1"); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errChannelSessionUnconfirmed) {
		t.Fatalf("读取失败的旧输入应保留重试：%v", err)
	}
}

func TestEventPaginationRejectsCursorCycles(t *testing.T) {
	for _, cursors := range [][]string{{"page_a", "page_a"}, {"page_a", "page_b", "page_a"}} {
		t.Run(fmt.Sprint(len(cursors)), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				if requests > len(cursors) {
					http.Error(w, "不应继续读取", http.StatusBadGateway)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"data":[{"id":"event_%d","type":"agent.message"}],"next_page":%q}`, requests, cursors[requests-1])
			}))
			defer server.Close()
			events, err := newOMAClient(server.URL).listEvents(t.Context(), "key", "sesn_1")
			if err == nil || len(events) != 0 || requests != len(cursors) {
				t.Fatalf("重复游标应拒绝不完整历史: err=%v events=%d requests=%d", err, len(events), requests)
			}
		})
	}
}

func TestEventPaginationPreservesLargeHistory(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		page := struct {
			Data     []omaEvent `json:"data"`
			NextPage string     `json:"next_page"`
		}{}
		start, count := 0, 1000
		if r.URL.Query().Get("page") == "next" {
			start, count = 1000, 1
		} else {
			page.NextPage = "next"
		}
		for i := range count {
			page.Data = append(page.Data, omaEvent{ID: fmt.Sprintf("event_%d", start+i), Type: "agent.message"})
		}
		if err := json.MarshalWrite(w, page); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	events, err := newOMAClient(server.URL).listEvents(t.Context(), "key", "sesn_1")
	if err != nil || len(events) != 1001 || requests != 2 || events[1000].ID != "event_1000" {
		t.Fatalf("跨1000事件分页丢失: err=%v events=%d requests=%d", err, len(events), requests)
	}
}

func TestHistoryPaginationRejectsCursorCycles(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests > 2 {
			http.Error(w, "不应继续读取", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"sesn_other","status":"idle"}],"next_page":"page_a"}`))
	}))
	defer server.Close()
	worker, channel := testWorker(t, &fakeRuntimeStore{}, server.URL)
	worker.listHistory(t.Context(), "chat:oc_1", &channelMessage{ChatType: "p2p", ChatID: "oc_1", UserID: "ou_1"}, "key", recoveryTarget{decision: identities.Decision{Mode: "legacy", AgentExternalID: worker.record.AgentExternalID}, environmentID: "env_1"})
	if requests != 2 || len(channel.sends) != 1 || !strings.Contains(channel.sends[0], "分页异常") {
		t.Fatalf("历史游标循环应明确拒绝: requests=%d replies=%v", requests, channel.sends)
	}
}
