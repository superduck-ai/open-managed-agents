package channels

import (
	"context"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/identities"
)

func TestSelfPairingChecksAdmissionAgainAndResumesOnlyTheConfirmedInput(t *testing.T) {
	server, creates, requests := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, feishu := testWorker(t, store, server.URL)
	worker.record.Mode = "pairing"
	store.record.Mode = "pairing"
	store.pairings = []db.ChannelPairingRecord{{Kind: "user", Target: "ou_1", Status: "pending"}}
	ingress := &pendingSelfPairing{}
	worker.supervisor.ingress = ingress
	msg := &channelMessage{MessageID: "om_original", ChatID: "oc_1", ChatType: "p2p", UserID: "ou_1", Content: "绑定后应继续执行的原请求"}
	if err := worker.receiveChannelMessage(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	if len(store.inputs) != 1 || store.inputs[store.inputOrder[0]].State != "pairing_wait" {
		t.Fatalf("未持久化待绑定消息: %+v", store.inputs)
	}
	cards := feishu.sentInputs()
	if len(cards) != 1 || !strings.Contains(cards[0].Card, "绑定我的身份") {
		t.Fatalf("未发送确认卡片: %v", cards)
	}
	worker.restoreInputs(t.Context())
	if len(requests.snapshot()) != 0 {
		t.Fatal("绑定之前已交接原请求")
	}
	event := &contracts.ActionEvent{
		ChatID: "oc_1", OpenID: "ou_1", MessageID: "om_card",
		Value: map[string]any{"kind": selfPairingAction, "channel_id": "chnl_1", "scope_key": "chat:oc_1", "open_id": "ou_1", "pairing_id": "chsp_pending", "source_message_id": msg.MessageID},
	}
	store.pairings[0].Status = "rejected"
	if err := worker.handleAction(t.Context(), event); err != nil || ingress.confirmed {
		t.Fatalf("撤销准入后卡片仍能绑定: %v %v", ingress.confirmed, err)
	}
	store.pairings = nil
	event.OpenID = "ou_other"
	if err := worker.handleAction(t.Context(), event); err == nil || ingress.confirmed {
		t.Fatal("其他操作者能完成绑定")
	}
	event.OpenID = "ou_1"
	if err := worker.handleAction(t.Context(), event); err != nil || !ingress.confirmed {
		t.Fatalf("准入后本人无法完成绑定: %v", err)
	}
	if !strings.Contains(strings.Join(feishu.appends, ""), "已继续处理刚才的消息。") {
		t.Fatal("首次确认没有说明原消息会继续处理")
	}
	if err := worker.handleAction(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	last := feishu.appends[len(feishu.appends)-1]
	if strings.Contains(last, "已继续处理刚才的消息。") || !strings.Contains(last, "身份已绑定。") {
		t.Fatalf("重复确认仍声称原消息已继续: %s", last)
	}
	worker.adapter.(*feishuChannel).Incoming.FlushAll(t.Context())
	waitForScope(t, worker, "chat:oc_1")
	sent := requests.snapshot()
	if creates.Load() != 0 || len(sent) != 1 || !strings.Contains(sent[0], msg.Content) {
		t.Fatalf("点击后没有恰好续跑一次原消息: %v", sent)
	}
}

type pendingSelfPairing struct {
	holdIngress
	confirmed bool
}

func (*pendingSelfPairing) AllowsSelfPairing(context.Context, identities.Ingress) (bool, error) {
	return true, nil
}

func (s *pendingSelfPairing) PrepareSelfPairing(context.Context, identities.Ingress) (string, error) {
	if s.confirmed {
		return "", nil
	}
	return "chsp_pending", nil
}
func (*pendingSelfPairing) Resolve(context.Context, identities.Ingress) (identities.Decision, error) {
	return identities.Decision{Mode: "legacy"}, nil
}
func (s *pendingSelfPairing) ConfirmSelfPairing(_ context.Context, ingress identities.Ingress, pairingID string, _ *int) error {
	s.confirmed = ingress.ActorID == "ou_1" && pairingID == "chsp_pending"
	return nil
}
