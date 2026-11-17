package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type artifactReceiptAdapter struct {
	channelAdapter
	attempts int
	rejected bool
	empty    bool
}

func (a *artifactReceiptAdapter) SendMedia(context.Context, *channelMessage, omaResource, string, []byte) (string, error) {
	a.attempts++
	if a.rejected {
		a.rejected = false
		return "", errChannelMediaUploadRejected
	}
	if a.empty {
		return "", nil
	}
	return fmt.Sprintf("om_file_%d", a.attempts), nil
}

func artifactDeliveryFixture(t *testing.T) (*channelWorker, *fakeRuntimeStore, *filesServer, *artifactReceiptAdapter, *channelReplyTask, *channelMessage) {
	t.Helper()
	files := &filesServer{resources: []omaResource{{ID: "output", Type: "file", FileID: "file_output", Filename: "result.txt", MimeType: "text/plain", MountPath: "/mnt/user-data/outputs/result.txt"}}}
	server := httptest.NewServer(files.handler(t))
	t.Cleanup(server.Close)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, _ := testWorker(t, store, server.URL)
	adapter := &artifactReceiptAdapter{channelAdapter: worker.adapter}
	worker.adapter = adapter
	state, err := json.Marshal(channelArtifactDelivery{Baseline: artifactBaseline{}})
	if err != nil {
		t.Fatal(err)
	}
	row := db.ChannelReplyDelivery{ChannelExternalID: "chnl_1", ChannelType: "feishu", AccountID: "app_1", InputEventID: "input", SessionExternalID: "sesn_1", ScopeKey: "chat:oc_1", State: "sent", ReplyMessageID: "card", ArtifactDelivery: string(state), LeaseOwner: worker.deliveryOwner, LeaseUntil: time.Now().Add(time.Hour)}
	store.deliveries = map[string]db.ChannelReplyDelivery{"chnl_1:input": row}
	task := &channelReplyTask{ctx: t.Context(), sessionID: "sesn_1", inputEventID: "input", scopeKey: "chat:oc_1", delivery: &row, terminal: true, normalEnd: true}
	msg := &channelMessage{MessageID: "message", ChatID: "oc_1", ChatType: "p2p", UserID: "actor"}
	return worker, store, files, adapter, task, msg
}

func decodeArtifactDelivery(t *testing.T, row db.ChannelReplyDelivery) channelArtifactDelivery {
	t.Helper()
	var state channelArtifactDelivery
	if err := json.Unmarshal([]byte(row.ArtifactDelivery), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestArtifactUploadRejectionResumesFrozenPlan(t *testing.T) {
	worker, store, files, adapter, task, msg := artifactDeliveryFixture(t)
	adapter.rejected = true
	worker.sendArtifacts(t.Context(), task, msg, "key")
	row := store.deliveries["chnl_1:input"]
	state := decodeArtifactDelivery(t, row)
	if row.State != "sent" || len(state.Files) != 1 || state.Files[0].State != "pending" || adapter.attempts != 1 {
		t.Fatalf("上传被拒绝后须保留原文件计划: %+v attempts=%d", state, adapter.attempts)
	}
	worker.sendArtifacts(t.Context(), task, msg, "key")
	if adapter.attempts != 1 {
		t.Fatal("重试时间未到，不得重复上传")
	}
	state.Files[0].RetryAt = time.Time{}
	payload, _ := json.Marshal(state)
	row.ArtifactDelivery, row.LeaseUntil = string(payload), time.Now().Add(-time.Second)
	store.deliveries["chnl_1:input"] = row
	worker.deliveryOwner = "restored-owner"
	claimed, ok, err := store.ClaimChannelReplyDelivery(t.Context(), worker.deliveryScope(), "input", worker.deliveryOwner)
	if err != nil || !ok {
		t.Fatalf("接管原交付: %v %v", ok, err)
	}
	task.delivery = &claimed
	files.resources = nil
	worker.sendArtifacts(t.Context(), task, msg, "key")
	final := store.deliveries["chnl_1:input"]
	state = decodeArtifactDelivery(t, final)
	if adapter.attempts != 2 || final.State != "completed" || state.Files[0].State != "sent" || state.Files[0].MessageID != "om_file_2" {
		t.Fatalf("恢复必须续传原计划并保存真实消息ID: %+v attempts=%d", state, adapter.attempts)
	}
}

type artifactCheckpointFailure struct {
	*fakeRuntimeStore
	failState string
	failed    bool
}

func (s *artifactCheckpointFailure) SaveChannelArtifactDelivery(ctx context.Context, scope db.ChannelDeliveryScope, inputID, owner, payload string) error {
	var state channelArtifactDelivery
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return err
	}
	if !s.failed && len(state.Files) > 0 && state.Files[0].State == s.failState {
		s.failed = true
		return errors.New("文件交接落库失败")
	}
	return s.fakeRuntimeStore.SaveChannelArtifactDelivery(ctx, scope, inputID, owner, payload)
}

func TestArtifactSendIntentAndReceiptFailureWindows(t *testing.T) {
	for _, failedState := range []string{"sending", "sent"} {
		t.Run(failedState, func(t *testing.T) {
			worker, store, files, adapter, task, msg := artifactDeliveryFixture(t)
			worker.supervisor.store = &artifactCheckpointFailure{fakeRuntimeStore: store, failState: failedState}
			worker.sendArtifacts(t.Context(), task, msg, "key")
			want := 0
			if failedState == "sent" {
				want = 1
			}
			if adapter.attempts != want {
				t.Fatalf("发送许可与副作用顺序错误: attempts=%d want=%d", adapter.attempts, want)
			}
			row := store.deliveries["chnl_1:input"]
			row.LeaseUntil = time.Now().Add(-time.Second)
			store.deliveries["chnl_1:input"] = row
			worker.deliveryOwner = "restored-owner"
			claimed, ok, err := store.ClaimChannelReplyDelivery(t.Context(), worker.deliveryScope(), "input", worker.deliveryOwner)
			if err != nil || !ok {
				t.Fatalf("交付接管失败: %v %v", ok, err)
			}
			task.delivery = &claimed
			files.resources = nil
			worker.sendArtifacts(t.Context(), task, msg, "key")
			state := decodeArtifactDelivery(t, store.deliveries["chnl_1:input"])
			if adapter.attempts != 1 || failedState == "sent" && state.Files[0].State != "unknown" || failedState == "sending" && state.Files[0].State != "sent" {
				t.Fatalf("已知未发送可继续，回执未知不得重放: attempts=%d state=%+v", adapter.attempts, state)
			}
		})
	}
}

func TestArtifactMissingMessageReceiptCannotReplay(t *testing.T) {
	worker, store, _, adapter, task, msg := artifactDeliveryFixture(t)
	adapter.empty = true
	worker.sendArtifacts(t.Context(), task, msg, "key")
	row := store.deliveries["chnl_1:input"]
	state := decodeArtifactDelivery(t, row)
	if adapter.attempts != 1 || state.Files[0].State != "unknown" {
		t.Fatalf("缺少平台消息ID必须记为未知: %+v", state)
	}
	task.delivery = &row
	worker.sendArtifacts(t.Context(), task, msg, "key")
	if adapter.attempts != 1 {
		t.Fatal("未知文件消息被重复发送")
	}
}

type artifactCloseStream struct{}

func (*artifactCloseStream) Receipt() string                               { return "card" }
func (*artifactCloseStream) Update(context.Context, contracts.Reply) error { return nil }
func (*artifactCloseStream) Close(context.Context) error                   { return nil }

func TestReplyCardCloseWaitsForArtifactDelivery(t *testing.T) {
	worker, store, _, adapter, task, msg := artifactDeliveryFixture(t)
	stream := &durableReplyStream{worker: worker, task: task, stream: &artifactCloseStream{}}
	if err := stream.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.deliveries["chnl_1:input"].State != "sent" || adapter.attempts != 0 {
		t.Fatal("卡片关闭不能提前把文件标为已交付")
	}
	worker.sendArtifacts(t.Context(), task, msg, "key")
	if store.deliveries["chnl_1:input"].State != "completed" || adapter.attempts != 1 {
		t.Fatal("文件回执保存后才允许完成交付")
	}
}

func TestArtifactBaselineCapturedBeforeInputAndPreserved(t *testing.T) {
	worker, store, _, _, _, msg := artifactDeliveryFixture(t)
	row := db.ChannelInput{UUID: "input", State: "prepared", LeaseOwner: worker.deliveryOwner, LeaseUntil: time.Now().Add(time.Hour)}
	store.inputs = map[string]db.ChannelInput{row.UUID: row}
	msg.Input, msg.Content = &row, "本轮原始内容"
	baseline, err := worker.captureArtifactBaseline(t.Context(), msg, "sesn_1", artifactBaseline{"old": "old-file:version"})
	if err != nil || len(baseline) != 1 || baseline["old"] != "old-file:version" {
		t.Fatalf("原产物基线未保存: %v %v", baseline, err)
	}
	baseline, err = worker.captureArtifactBaseline(t.Context(), msg, "sesn_1", artifactBaseline{"old": "old-file:version", "new": "new-file:version"})
	if err != nil || len(baseline) != 1 || baseline["old"] != "old-file:version" {
		t.Fatalf("恢复不得以当前资源覆盖原基线: %v %v", baseline, err)
	}
	if _, _, err := worker.inputContent(t.Context(), "key", "sesn_1", msg); err != nil {
		t.Fatal(err)
	}
	var prepared channelPreparedInput
	if err := json.Unmarshal([]byte(msg.Input.PreparedContent), &prepared); err != nil || prepared.ArtifactBaseline["old"] != "old-file:version" {
		t.Fatalf("正文准备丢失原产物基线: %v", err)
	}
}

func TestMissingArtifactBaselineDoesNotBecomeKnownEmpty(t *testing.T) {
	worker, _, _, _, _, msg := artifactDeliveryFixture(t)
	row, err := worker.prepareReplyDelivery(t.Context(), &channelReplyTask{inputEventID: "legacy_input", sessionID: "sesn_1"}, msg)
	if err != nil {
		t.Fatal(err)
	}
	state := decodeArtifactDelivery(t, row)
	if state.Baseline != nil {
		t.Fatal("未知旧基线不能被JSON编码变成已知空基线")
	}
}

func sendTestArtifacts(t *testing.T, worker *channelWorker, task *channelReplyTask, msg *channelMessage, baseline artifactBaseline) {
	t.Helper()
	if task.delivery == nil {
		store := worker.supervisor.store.(*fakeRuntimeStore)
		if store.deliveries == nil {
			store.deliveries = map[string]db.ChannelReplyDelivery{}
		}
		task.ctx = t.Context()
		task.inputEventID = fmt.Sprintf("input_%d", len(store.deliveries))
		state, err := json.Marshal(channelArtifactDelivery{Baseline: baseline})
		if err != nil {
			t.Fatal(err)
		}
		row := db.ChannelReplyDelivery{InputEventID: task.inputEventID, State: "sent", ReplyMessageID: "card", ArtifactDelivery: string(state), LeaseOwner: worker.deliveryOwner, LeaseUntil: time.Now().Add(time.Hour)}
		store.deliveries[worker.record.ExternalID+":"+task.inputEventID] = row
		task.delivery = &row
	}
	worker.sendArtifacts(t.Context(), task, msg, "key")
}
