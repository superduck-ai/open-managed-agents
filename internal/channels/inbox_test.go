package channels

import (
	"context"
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestInputRecoveryDistinguishesUserCancellationFromShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "用户停止不应复活", true: "服务停止保留原输入"}[shutdown], func(t *testing.T) {
			server, _, requests := omaTestServer(t, 0)
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, server.URL)
			root, stop := context.WithCancel(t.Context())
			defer stop()
			worker.ctx = root
			msg := &channelMessage{MessageID: "cancelled", MessageIDs: []string{"cancelled"}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: "尚未提交"}
			payload, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			row, _, err := store.ReceiveChannelInput(t.Context(), worker.inputScope(), db.ChannelInput{MessageID: msg.MessageID, ScopeKey: "chat:oc_1", Payload: string(payload)})
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err := store.ClaimChannelInput(t.Context(), worker.inputScope(), row.UUID, worker.deliveryOwner); err != nil || !found {
				t.Fatalf("未认领输入: %v %v", found, err)
			}
			row, err = store.PrepareChannelInputBatch(t.Context(), worker.inputScope(), worker.deliveryOwner, db.ChannelInputBatch{MessageIDs: msg.MessageIDs, ScopeKey: "chat:oc_1", Payload: string(payload)})
			if err != nil {
				t.Fatal(err)
			}
			msg.input = &row
			task := worker.startReplyTask(root, "chat:oc_1", "sesn_1")
			if shutdown {
				stop()
			} else {
				task.cancel()
			}
			worker.deliver(task, msg, "key")
			want := "handed_off"
			if shutdown {
				want = "prepared"
			}
			store.mu.Lock()
			state := store.inputs[row.UUID].State
			store.mu.Unlock()
			if state != want || len(requests.snapshot()) != 0 {
				t.Fatalf("取消语义错误: state=%s want=%s requests=%v", state, want, requests.snapshot())
			}
		})
	}
}

func TestChannelInputRecoveryAtHandoffBoundaries(t *testing.T) {
	for _, phase := range []string{"received", "buffering", "prepared", "content", "accepted"} {
		t.Run(phase, func(t *testing.T) {
			server, creates, requests := omaTestServer(t, 0)
			store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
			worker, _ := testWorker(t, store, server.URL)
			msg := &channelMessage{MessageID: "source", MessageIDs: []string{"source"}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: "原始待交接输入"}
			payload, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			row, _, err := store.ReceiveChannelInput(t.Context(), worker.inputScope(), db.ChannelInput{MessageID: msg.MessageID, ScopeKey: "chat:oc_1", Payload: string(payload)})
			if err != nil {
				t.Fatal(err)
			}
			if phase != "received" {
				if _, ok, err := store.ClaimChannelInput(t.Context(), worker.inputScope(), row.UUID, "departed"); err != nil || !ok {
					t.Fatalf("原实例未认领: %v %v", ok, err)
				}
			}
			if phase != "received" && phase != "buffering" {
				if _, err := store.PrepareChannelInputBatch(t.Context(), worker.inputScope(), "departed", db.ChannelInputBatch{MessageIDs: msg.MessageIDs, ScopeKey: "chat:oc_1", Payload: string(payload)}); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "content" || phase == "accepted" {
				change := db.ChannelInputAdvance{State: "prepared", SessionExternalID: "sesn_1", PreparedContent: `{"content":[{"type":"text","text":"已准备的原始内容"}],"skipped":[],"mounted":{}}`}
				if phase == "accepted" {
					change.State, change.InputEventID, change.PreparedContent = "accepted", "evt_user_1", ""
				}
				if _, err := store.AdvanceChannelInput(t.Context(), worker.inputScope(), row.UUID, "departed", change); err != nil {
					t.Fatal(err)
				}
			}
			store.mu.Lock()
			pending := store.inputs[row.UUID]
			pending.LeaseUntil = time.Now().Add(-time.Second)
			store.inputs[row.UUID] = pending
			store.mu.Unlock()
			worker.restoreInputs(t.Context())
			worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
			waitForScope(t, worker, "chat:oc_1")
			want := 1
			if phase == "accepted" {
				want = 0
			}
			if len(requests.snapshot()) != want || creates.Load() != 0 {
				t.Fatalf("恢复重复提交或替换了会话: requests=%v creates=%d", requests.snapshot(), creates.Load())
			}
			if phase == "content" && !strings.Contains(requests.snapshot()[0], "已准备的原始内容") {
				t.Fatalf("恢复没有复用已准备内容: %v", requests.snapshot())
			}
			store.mu.Lock()
			final := store.inputs[row.UUID]
			store.mu.Unlock()
			if final.State != "handed_off" || strings.Contains(final.Payload, `"content"`) || final.PreparedContent != "" {
				t.Fatalf("交接后未清理临时正文: %+v", final)
			}
		})
	}
}

func TestChannelInputDuplicateDoesNotReenterBatch(t *testing.T) {
	server, _, requests := omaTestServer(t, 0)
	store := &fakeRuntimeStore{}
	worker, _ := testWorker(t, store, server.URL)
	for range 3 {
		if err := worker.receiveChannelMessage(t.Context(), &channelMessage{MessageID: "same", ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: "同一条原消息"}); err != nil {
			t.Fatal(err)
		}
	}
	worker.adapter.(*feishuChannel).incoming.FlushAll(t.Context())
	waitForScope(t, worker, "chat:oc_1")
	if len(requests.snapshot()) != 1 || strings.Count(requests.snapshot()[0], "同一条原消息") != 1 {
		t.Fatalf("重复回调进入了同一批次: %v", requests.snapshot())
	}
}

func TestRestartPreservesPreparedBatchWithoutPlatformSDK(t *testing.T) {
	server, creates, requests := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	worker.record.ChannelType = "other"
	worker.adapter = &independentAdapter{worker: worker}
	var anchor db.ChannelInput
	for _, id := range []string{"a", "b"} {
		msg := &channelMessage{MessageID: id, MessageIDs: []string{id}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: id}
		payload, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		row, _, err := store.ReceiveChannelInput(t.Context(), worker.inputScope(), db.ChannelInput{MessageID: id, ScopeKey: "chat:oc_1", Payload: string(payload)})
		if err != nil {
			t.Fatal(err)
		}
		if _, found, err := store.ClaimChannelInput(t.Context(), worker.inputScope(), row.UUID, "departed"); err != nil || !found {
			t.Fatalf("原实例未认领: %v %v", found, err)
		}
		anchor = row
	}
	msg := &channelMessage{MessageID: "b", MessageIDs: []string{"a", "b"}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: "甲\n\n乙"}
	payload, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareChannelInputBatch(t.Context(), worker.inputScope(), "departed", db.ChannelInputBatch{MessageIDs: msg.MessageIDs, ScopeKey: "chat:oc_1", Payload: string(payload)}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	row := store.inputs[anchor.UUID]
	row.LeaseUntil = time.Now().Add(-time.Second)
	store.inputs[anchor.UUID] = row
	store.mu.Unlock()
	worker.restoreInputs(t.Context())
	waitForScope(t, worker, "chat:oc_1")
	if len(requests.snapshot()) != 1 || creates.Load() != 0 || !strings.Contains(requests.snapshot()[0], `甲\n\n乙`) || !strings.Contains(requests.snapshot()[0], `"channel_message_ids":["a","b"]`) || !strings.Contains(requests.snapshot()[0], `"channel_type":"other"`) {
		t.Fatalf("恢复不能拆散原批次或依赖飞书SDK: %v creates=%d", requests.snapshot(), creates.Load())
	}
}
