package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type channelActionValue = contracts.ActionValue

func (w *channelWorker) cardValue(task *channelReplyTask, toolUseID, kind, openID, threadID string) channelActionValue {
	return channelActionValue{
		Kind:      kind,
		ChannelID: w.record.ExternalID,
		ScopeKey:  task.scopeKey,
		SessionID: task.sessionID,
		ToolUseID: toolUseID,
		OpenID:    openID,
		ThreadID:  threadID,
	}
}

func answeredToolIDs(events []omaEvent) map[string]struct{} {
	answered := make(map[string]struct{})
	for _, event := range events {
		switch event.Type {
		case "user.tool_confirmation":
			if id := eventToolUseID(event); id != "" {
				answered[id] = struct{}{}
			}
		case "user.custom_tool_result":
			if id := eventToolUseID(event); id != "" {
				answered[id] = struct{}{}
			}
		}
	}
	return answered
}

func eventToolUseID(event omaEvent) string {
	for _, id := range []string{event.ToolUseID, event.MCPToolUseID, event.CustomToolUseID} {
		if id != "" {
			return id
		}
	}
	return ""
}

func (w *channelWorker) handleAction(ctx context.Context, event *contracts.ActionEvent) error {
	if event == nil {
		return nil
	}
	value, err := decodeActionValue(event.Value)
	if err != nil {
		return err
	}
	if value.Kind == selfPairingAction {
		return w.confirmSelfPairing(ctx, event, value)
	}
	if value.SessionID == "" || value.ToolUseID == "" {
		return nil
	}
	if value.ChannelID != w.record.ExternalID {
		return fmt.Errorf("渠道卡片不属于当前渠道")
	}
	operator := event.OpenID
	if value.OpenID != "" && value.OpenID != operator {
		return fmt.Errorf("只有发起人本人可以操作该卡片")
	}
	if event.ChatID != "" && !scopeMatchesChat(value.ScopeKey, event.ChatID) {
		return fmt.Errorf("渠道卡片不属于当前会话")
	}
	receipt, err := w.cardActionReceipt(ctx, value, event.MessageID, event.ChatID, operator)
	if err != nil {
		return err
	}
	value.ChatType = receipt.ChatType
	value.OpenID = receipt.ActorID
	allowed, authErr := w.cardAuthorized(ctx, value, operator, event.ChatID)
	if authErr != nil {
		return authErr
	}
	if !allowed {
		w.notifyChat(ctx, event.ChatID, "当前会话未获授权，操作已拒绝。")
		return fmt.Errorf("当前会话未获授权")
	}
	if w.currentSession(ctx, value.ScopeKey) != value.SessionID {
		return fmt.Errorf("该操作已失效，请重新发起")
	}
	value.RequestKey = cardControlRequestKey(receipt, value.ToolUseID)
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return err
	}
	events, err := w.supervisor.oma.listEvents(ctx, apiKey, value.SessionID)
	if err != nil {
		return err
	}
	events, err = activeCardEvents(events, receipt.InputEventID)
	if err != nil {
		return err
	}
	if value.Kind == "stop" {
		if value.ToolUseID != receipt.InputEventID {
			return invalidChannelCardReceipt()
		}
		return w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, map[string]any{"type": "user.interrupt"}, value.RequestKey)
	}
	if _, done := answeredToolIDs(events)[value.ToolUseID]; done {
		return nil
	}
	switch value.Kind {
	case "cancel_oauth":
		err := w.cancelOAuthCardAction(ctx, apiKey, value, events)
		if err == nil {
			w.resumeReplyDelivery(receipt)
		}
		return err
	case "confirm":
		if err := w.confirmCardAction(ctx, apiKey, value, events); err != nil {
			return err
		}
		w.resumeReplyDelivery(receipt)
		return nil
	case "answer", "cancel_question":
		err := w.answerQuestion(ctx, apiKey, value, event.FormValue, events)
		if public, ok := errors.AsType[*apperr.Error](err); ok {
			return w.replyText(ctx, &channelMessage{ChatID: receipt.ChatID, ChatType: receipt.ChatType, MessageID: receipt.ReplyMessageID}, public.PublicMessage)
		}
		if err == nil {
			w.resumeReplyDelivery(receipt)
		}
		return err
	default:
		return nil
	}
}

func scopeMatchesChat(scopeKey, chatID string) bool {
	if scopeKey == "chat:"+chatID {
		return true
	}
	return strings.HasPrefix(scopeKey, "thread:"+chatID+":")
}

func decodeActionValue(raw map[string]any) (channelActionValue, error) {
	if raw == nil {
		return channelActionValue{}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return channelActionValue{}, err
	}
	var value channelActionValue
	if err := json.Unmarshal(encoded, &value); err != nil {
		return channelActionValue{}, fmt.Errorf("渠道卡片参数无效: %w", err)
	}
	return value, nil
}

func isQuestionTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "askuserquestion", "askuserquestions":
		return true
	default:
		return false
	}
}

func (w *channelWorker) notifyChat(ctx context.Context, chatID, text string) {
	if chatID == "" {
		return
	}
	if err := w.replyText(ctx, &channelMessage{ChatID: chatID}, text); err != nil {
		w.supervisor.logger.DebugContext(ctx, "send channel notice", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) cardActionReceipt(ctx context.Context, value channelActionValue, messageID, chatID, actorID string) (db.ChannelReplyDelivery, error) {
	if messageID == "" || chatID == "" || actorID == "" {
		return db.ChannelReplyDelivery{}, invalidChannelCardReceipt()
	}
	row, err := w.supervisor.store.GetChannelReplyDeliveryByReceipt(ctx, w.deliveryScope(), messageID)
	if errors.Is(err, db.ErrNotFound) {
		return db.ChannelReplyDelivery{}, invalidChannelCardReceipt()
	}
	if err != nil {
		return db.ChannelReplyDelivery{}, err
	}
	if row.State != "sent" || row.ChannelType != w.record.ChannelType || row.AccountID != w.record.AppID || row.SessionExternalID != value.SessionID || row.ScopeKey != value.ScopeKey || row.ChatID != chatID || row.ActorID != actorID {
		return db.ChannelReplyDelivery{}, invalidChannelCardReceipt()
	}
	return row, nil
}

func activeCardEvents(events []omaEvent, inputID string) ([]omaEvent, error) {
	start := -1
	for index, event := range events {
		if event.ID == inputID && event.Type == "user.message" && string(event.ProcessedAt) != "null" {
			start = index + 1
			break
		}
	}
	if start < 0 {
		return nil, invalidChannelCardReceipt()
	}
	current := events[start:]
	for _, event := range current {
		if event.Type == "user.message" && string(event.ProcessedAt) != "null" || terminalEvent(event) {
			return nil, invalidChannelCardReceipt()
		}
	}
	return current, nil
}

func (w *channelWorker) confirmCardAction(ctx context.Context, apiKey string, value channelActionValue, events []omaEvent) error {
	var request omaEvent
	for _, event := range events {
		if event.ID == value.ToolUseID {
			request = event
			break
		}
	}
	if request.Permission != "ask" || request.Type != "agent.tool_use" && request.Type != "agent.mcp_tool_use" {
		return invalidChannelCardReceipt()
	}
	result := "deny"
	if value.Result == "allow" {
		result = "allow"
	}
	confirmation := map[string]any{"type": "user.tool_confirmation", "tool_use_id": request.ID, "result": result}
	if request.SessionThreadID != "" {
		confirmation["session_thread_id"] = request.SessionThreadID
	}
	return w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, confirmation, value.RequestKey)
}

func (w *channelWorker) cancelOAuthCardAction(ctx context.Context, apiKey string, value channelActionValue, events []omaEvent) error {
	for _, request := range events {
		if request.ID != value.ToolUseID || request.Type != "agent.custom_tool_use" || request.Name != "RequestOAuth" || value.ThreadID != "" && request.SessionThreadID != value.ThreadID {
			continue
		}
		result := map[string]any{
			"type": "user.custom_tool_result", "custom_tool_use_id": value.ToolUseID,
			"is_error": true,
			"content":  []map[string]string{{"type": "text", "text": "用户取消了目标服务授权。"}},
		}
		if request.SessionThreadID != "" {
			result["session_thread_id"] = request.SessionThreadID
		}
		return w.supervisor.oma.sendEvent(ctx, apiKey, value.SessionID, result, value.RequestKey)
	}
	return invalidChannelCardReceipt()
}
