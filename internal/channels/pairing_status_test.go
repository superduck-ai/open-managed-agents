package channels

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPairingReplyMatchesActualAuthorization(t *testing.T) {
	for _, chatType := range []string{"group", "p2p"} {
		for _, tc := range []struct {
			status, reply string
			allowed       bool
		}{
			{status: "approved", allowed: true},
			{status: "pending", reply: "配对申请正在等待管理员批准。"},
			{status: "rejected", reply: "当前聊天未获授权，请联系管理员重新批准配对。"},
		} {
			t.Run(chatType+"/"+tc.status, func(t *testing.T) {
				kind, target := channelPairTarget(chatType, "chat", "user")
				store := &fakeRuntimeStore{pairings: []db.ChannelPairingRecord{{Kind: kind, Target: target, Status: tc.status}}}
				worker, channel := testWorker(t, store, "")
				worker.record.Mode = "pairing"
				allowed, err := worker.authorize(t.Context(), &channelMessage{ChatType: chatType, ChatID: "chat", UserID: "user"})
				if err != nil || allowed != tc.allowed {
					t.Fatalf("放行状态错误: %v %v", allowed, err)
				}
				inputs := channel.sentInputs()
				if tc.allowed {
					if len(inputs) != 0 {
						t.Fatalf("已批准的消息不应收到配对拦截提示: %+v", inputs)
					}
				} else if len(inputs) != 1 || inputs[0].Text != tc.reply {
					t.Fatalf("提示与实际授权状态不符: %+v", inputs)
				}
				allowed, err = worker.cardAuthorized(t.Context(), channelActionValue{ChatType: chatType}, "user", "chat")
				if err != nil || allowed != tc.allowed {
					t.Fatalf("卡片授权状态错误: %v %v", allowed, err)
				}
			})
		}
	}
}
