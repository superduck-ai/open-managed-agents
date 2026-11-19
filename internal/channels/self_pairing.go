package channels

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const selfPairingAction = "pair_identity"

func (w *channelWorker) confirmSelfPairing(ctx context.Context, event *contracts.ActionEvent, value channelActionValue) error {
	if w.supervisor.ingress == nil || value.ChannelID != w.record.ExternalID || value.PairingID == "" || value.SourceID == "" ||
		event.OpenID == "" || value.OpenID != event.OpenID || event.ChatID == "" ||
		value.ScopeKey != "chat:"+event.ChatID || event.MessageID == "" {
		return invalidChannelCardReceipt()
	}
	record, err := w.supervisor.store.GetChannelRecord(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID, w.record.ExternalID)
	if err != nil {
		return err
	}
	if !record.Enabled || record.DeletedAt != nil || record.BindingStatus != "bound" || record.AppID != w.record.AppID || record.ChannelType != "feishu" {
		return invalidChannelCardReceipt()
	}
	msg := &channelMessage{ChatID: event.ChatID, ChatType: "p2p", UserID: event.OpenID}
	if record.Mode == "pairing" {
		decision, err := w.pairingFor(ctx, "user", event.OpenID)
		if err != nil {
			return err
		}
		if decision == pairingRejected {
			w.notifyChat(ctx, event.ChatID, "当前私聊未获渠道授权，无法绑定执行身份。")
			return nil
		}
	}
	input := db.ChannelPairingResume{MessageID: value.SourceID, ScopeKey: value.ScopeKey, ActorID: event.OpenID, PairingID: value.PairingID}
	waiting, err := w.supervisor.store.HasWaitingChannelPairingInput(ctx, w.inputScope(), input)
	if err != nil {
		return err
	}
	if !waiting {
		return w.replyText(ctx, msg, "这条消息已处理或配对已失效。")
	}
	var version *int
	if fixed := parseChannelMounts(record.Mounts).AgentVersion; fixed > 0 {
		version = &fixed
	}
	err = w.supervisor.ingress.ConfirmSelfPairing(ctx, channelIngress(record, msg, value.ScopeKey), value.PairingID, version)
	if public, ok := errors.AsType[*apperr.Error](err); ok {
		return w.replyText(ctx, msg, public.PublicMessage)
	}
	if err != nil {
		return err
	}
	id, resumed, err := w.supervisor.store.ResumeChannelInputAfterPairing(ctx, w.inputScope(), input)
	if err != nil {
		return err
	}
	text := "身份已绑定。"
	if resumed {
		if err := w.claimAndDispatchInput(ctx, id); err != nil {
			return err
		}
		text = "已继续处理刚才的消息。"
	}
	return w.adapter.ResumeReply(event.MessageID).Update(ctx, contracts.Reply{
		Status: "绑定成功", Ended: true,
		Blocks: []contracts.ReplyBlock{{Text: text}},
	})
}
