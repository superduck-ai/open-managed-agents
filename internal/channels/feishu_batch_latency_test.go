package channels

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type batchLatencyStore struct {
	*fakeRuntimeStore
	prepared chan db.ChannelInputBatch
	failure  error
}

func (s *batchLatencyStore) PrepareChannelInputBatch(_ context.Context, _ db.ChannelInputScope, _ string, batch db.ChannelInputBatch) (db.ChannelInput, error) {
	s.prepared <- batch
	return db.ChannelInput{}, s.failure
}

func TestFeishuIdleInputBypassesBatchDelay(t *testing.T) {
	for _, activeScope := range []string{"chat:another", ""} {
		t.Run(activeScope, func(t *testing.T) {
			worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
			failure := errors.New("已到达准备入口")
			store := &batchLatencyStore{fakeRuntimeStore: &fakeRuntimeStore{}, prepared: make(chan db.ChannelInputBatch, 1), failure: failure}
			worker.supervisor.store = store
			adapter := worker.adapter.(*feishuChannel)
			adapter.Incoming.Dispose()
			config := channeltypes.DefaultBatchConfig()
			config.DelayMs, config.LongDelayMs = time.Hour, time.Hour
			adapter.Incoming = newFeishuInputPipelines(config)
			t.Cleanup(adapter.Incoming.Dispose)
			if activeScope != "" {
				ticket := worker.enqueueScope(activeScope)
				defer worker.releaseScope(ticket)
			}
			msg := &channelMessage{MessageID: "first", MessageIDs: []string{"first"}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: "空闲首条", Input: &db.ChannelInput{ScopeKey: "chat:oc_1"}}
			if err := adapter.QueueInput(t.Context(), msg); !errors.Is(err, failure) {
				t.Fatalf("空闲会话首条消息不应等待合并计时器: %v", err)
			}
			select {
			case batch := <-store.prepared:
				if !slices.Equal(batch.MessageIDs, []string{"first"}) {
					t.Fatalf("首条输入身份丢失: %v", batch.MessageIDs)
				}
			default:
				t.Fatal("首条消息尚未准备")
			}
		})
	}
}

func TestFeishuBusyInputKeepsBatchWindow(t *testing.T) {
	for _, initial := range []string{"空闲首条", "/help"} {
		t.Run(initial, func(t *testing.T) {
			worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
			store := &batchLatencyStore{fakeRuntimeStore: &fakeRuntimeStore{}, prepared: make(chan db.ChannelInputBatch, 3), failure: errors.New("停止准备")}
			worker.supervisor.store = store
			adapter := worker.adapter.(*feishuChannel)
			adapter.Incoming.Dispose()
			config := channeltypes.DefaultBatchConfig()
			config.DelayMs, config.LongDelayMs = time.Hour, time.Hour
			adapter.Incoming = newFeishuInputPipelines(config)
			t.Cleanup(adapter.Incoming.Dispose)
			first := &channelMessage{MessageID: "first", MessageIDs: []string{"first"}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: initial, Input: &db.ChannelInput{ScopeKey: "chat:oc_1"}}
			if err := adapter.QueueInput(t.Context(), first); !errors.Is(err, store.failure) {
				t.Fatalf("首条未准备: %v", err)
			}
			<-store.prepared
			ticket := worker.enqueueScope("chat:oc_1")
			defer worker.releaseScope(ticket)
			for _, id := range []string{"second", "third"} {
				if err := adapter.QueueInput(t.Context(), &channelMessage{MessageID: id, MessageIDs: []string{id}, ChatID: "oc_1", ChatType: "p2p", UserID: "actor", Content: id, Input: &db.ChannelInput{ScopeKey: "chat:oc_1"}}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-store.prepared:
				t.Fatal("运行中的后续输入应保留合并窗口")
			default:
			}
			adapter.Incoming.FlushAll(t.Context())
			batch := <-store.prepared
			if !slices.Equal(batch.MessageIDs, []string{"second", "third"}) {
				t.Fatalf("后续消息未按原顺序合并: %v", batch.MessageIDs)
			}
		})
	}
}
