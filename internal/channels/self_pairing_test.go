package channels

import (
	"context"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/identities"
)

func TestSelfPairingDispatchesOriginalMessageWithoutCard(t *testing.T) {
	server, creates, requests := omaTestServer(t, 0)
	store := &fakeRuntimeStore{bindings: map[string]string{"chat:oc_1": "sesn_1"}}
	worker, feishu := testWorker(t, store, server.URL)
	worker.record.Mode = "pairing"
	store.record.Mode = "pairing"
	store.pairings = []db.ChannelPairingRecord{{Kind: "user", Target: "ou_1", Status: "rejected"}}
	worker.supervisor.ingress = autoPairingIngress{}
	msg := &channelMessage{MessageID: "om_original", ChatID: "oc_1", ChatType: "p2p", UserID: "ou_1", Content: "自动配对后应执行的原消息"}
	if err := worker.receiveChannelMessage(t.Context(), msg); err != nil || len(store.inputs) != 0 {
		t.Fatalf("拒绝准入的消息进入了收件箱：%v", err)
	}
	store.pairings = nil
	for range 2 {
		if err := worker.receiveChannelMessage(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	worker.adapter.(*feishuChannel).Incoming.FlushAll(t.Context())
	waitForScope(t, worker, "chat:oc_1")
	sent := requests.snapshot()
	if creates.Load() != 0 || len(sent) != 1 || !strings.Contains(sent[0], msg.Content) {
		t.Fatalf("原消息应只执行一次：%v", sent)
	}
	for _, card := range feishu.sentInputs() {
		if strings.Contains(card.Card, "绑定我的身份") {
			t.Fatal("自动配对仍发送旧确认卡片")
		}
	}
}

type autoPairingIngress struct{ holdIngress }

func (autoPairingIngress) AllowsSelfPairing(context.Context, identities.Ingress) (bool, error) {
	return true, nil
}

func (autoPairingIngress) Resolve(context.Context, identities.Ingress) (identities.Decision, error) {
	return identities.Decision{Mode: "legacy"}, nil
}
