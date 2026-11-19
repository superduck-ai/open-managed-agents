package channels

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
)

// 发送回执随原回复保存。重放 RequestOAuth 时继续使用同一张飞书卡片。
func (r *replyRunner) detachOAuthCard(ctx context.Context, view *contracts.Reply) error {
	if r.task.delivery == nil || r.msg.ChatType != "p2p" || view.OAuthOrigin == "" || view.Ended || view.Interrupted {
		return nil
	}
	for _, action := range view.Interactions {
		if action.Kind != "oauth" {
			continue
		}
		var request codesessions.OAuthRequest
		if json.Unmarshal([]byte(action.Input), &request) != nil || request.Validate() != nil {
			continue
		}
		name := oauthServiceName(request)
		id := action.Value.ToolUseID
		if r.oauthCards[id] == "" {
			action.Value.SourceID = r.task.delivery.ReplyMessageID
			card := contracts.Reply{Title: "连接 " + name, Status: "等待授权", OAuthOrigin: view.OAuthOrigin, Interactions: []contracts.Interaction{action}, DeliveryKey: id}
			if view.StopValue != nil {
				stop := *view.StopValue
				stop.SourceID = action.Value.SourceID
				card.StopValue = &stop
			}
			stream, err := r.worker.adapter.OpenReply(ctx, r.msg, card)
			if err != nil {
				return err
			}
			receipt := stream.Receipt()
			if err := stream.Close(ctx); err != nil {
				return err
			}
			if receipt == "" {
				return errors.New("授权卡缺少发送回执")
			}
			var receipts channelArtifactDelivery
			if err := json.Unmarshal([]byte(r.task.delivery.ArtifactDelivery), &receipts); err != nil {
				return err
			}
			if receipts.OAuthCards == nil {
				receipts.OAuthCards = make(map[string]string)
			}
			receipts.OAuthCards[id] = receipt
			if err := r.worker.saveArtifactDelivery(ctx, r.task, &receipts); err != nil {
				return err
			}
			r.oauthCards = receipts.OAuthCards
		}
		view.Status = "已请求 " + name + " 授权"
		view.StopValue = nil
	}
	view.Interactions = slices.DeleteFunc(view.Interactions, func(action contracts.Interaction) bool { return r.oauthCards[action.Value.ToolUseID] != "" })
	return nil
}

func (r *replyRunner) settleOAuthCards(ctx context.Context, event omaEvent) error {
	for _, pending := range r.reply.interactions {
		receipt := r.oauthCards[pending.ID]
		if receipt == "" {
			continue
		}
		status := "已结束授权"
		if event.Type == "user.custom_tool_result" && event.CustomToolUseID == pending.ID {
			if event.IsError {
				status = "连接失效"
				if eventText(event) == cancelledOAuthMessage {
					status = "已取消授权"
				}
			} else {
				status = "连接失效"
				if codesessions.OAuthGranted(eventText(event)) {
					status = "已连接"
				}
			}
		} else if !r.task.terminal && !terminalEvent(event) && event.Type != "user.interrupt" {
			continue
		}
		var input codesessions.OAuthRequest
		if err := json.Unmarshal(pending.Input, &input); err != nil {
			return err
		}
		stream := r.worker.adapter.ResumeReply(receipt)
		if err := updateChannelReply(ctx, stream, contracts.Reply{Title: "连接 " + oauthServiceName(input), Status: status, Ended: true, Blocks: []contracts.ReplyBlock{{Text: status}}}); err != nil {
			return err
		}
		if status == "已连接" {
			if err := r.continueAfterOAuth(ctx, pending.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func oauthServiceName(input codesessions.OAuthRequest) string {
	return cmp.Or(input.ProviderName, input.MCPServerName, input.Provider, input.TargetHost)
}

func (r *replyRunner) continueAfterOAuth(ctx context.Context, requestID string) error {
	delivery := r.task.delivery
	var receipts channelArtifactDelivery
	if err := json.Unmarshal([]byte(delivery.ArtifactDelivery), &receipts); err != nil {
		return err
	}
	id := receipts.Continuations[requestID]
	if id == "" {
		key := sha256.Sum256([]byte("continued:" + requestID))
		stream, err := r.worker.adapter.OpenReply(ctx, r.msg, contracts.Reply{Status: "正在继续查询", DeliveryKey: fmt.Sprintf("%x", key[:16])})
		if err != nil {
			return err
		}
		id = stream.Receipt()
		if err := stream.Close(ctx); err != nil {
			return err
		}
		if id == "" {
			return errors.New("续跑回复卡缺少发送回执")
		}
		if receipts.Continuations == nil {
			receipts.Continuations = make(map[string]string)
		}
		receipts.Continuations[requestID] = id
		if err := r.worker.saveArtifactDelivery(ctx, r.task, &receipts); err != nil {
			return err
		}
	}
	// 后续事件仍经原交付租约写入，只切换飞书消息。
	r.stream.(*durableReplyStream).stream = r.worker.adapter.ResumeReply(id)
	r.reply.stopValue.SourceID = delivery.ReplyMessageID
	return nil
}
