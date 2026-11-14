package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/safety"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"slices"
	"strings"
)

type feishuChannel struct {
	worker   *channelWorker
	client   *lark.Client
	channel  channeltypes.Channel
	incoming *feishuInputPipelines
}

func (f *feishuChannel) start(ctx context.Context) error { return f.channel.Start(ctx) }
func (f *feishuChannel) close(ctx context.Context) error {
	f.incoming.Dispose()
	return f.channel.Stop(ctx)
}

func feishuMessage(msg *channeltypes.NormalizedMessage) *channelMessage {
	if msg == nil {
		return nil
	}
	resources := make([]channelAttachment, len(msg.Resources))
	for i, r := range msg.Resources {
		resources[i] = channelAttachment{Type: r.Type, FileKey: r.FileKey, FileName: r.FileName}
	}
	rootID, threadID := "", ""
	if msg.ChatType != "p2p" {
		rootID = channelMessageID(msg, []string{"root_id"})
		threadID = channelMessageID(msg, []string{"thread_id", "root_id"})
	}
	return &channelMessage{MessageID: msg.MessageID, MessageIDs: []string{msg.MessageID}, ChatID: msg.ChatID, ChatType: msg.ChatType, UserID: msg.UserID, Content: strings.TrimSpace(msg.Content), RawContentType: msg.RawContentType, RootID: rootID, ThreadID: threadID, Resources: resources}
}

func channelMessageID(msg *channeltypes.NormalizedMessage, keys []string) string {
	if raw, ok := msg.RawEvent.(map[string]any); ok {
		return messageIDFromPayload(raw, keys)
	}
	if msg.RawEvent == nil {
		return ""
	}
	encoded, err := json.Marshal(msg.RawEvent)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return ""
	}
	return messageIDFromPayload(payload, keys)
}

func messageIDFromPayload(payload map[string]any, keys []string) string {
	if value := messageIDFromMessage(payload, keys); value != "" {
		return value
	}
	for _, key := range []string{"message", "event"} {
		nested, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		if value := messageIDFromMessage(nested, keys); value != "" {
			return value
		}
		if message, ok := nested["message"].(map[string]any); ok {
			if value := messageIDFromMessage(message, keys); value != "" {
				return value
			}
		}
	}
	return ""
}

func messageIDFromMessage(payload map[string]any, keys []string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func newFeishuWSClient(appID, secret string) *larkws.Client {
	return larkws.NewClient(appID, secret, larkws.WithEventHandler(larkevent.NewEventDispatcher("", "")))
}

func newFeishuChannel(worker *channelWorker, secret string) (channelAdapter, error) {
	if worker.record.AppID == "" || secret == "" {
		return nil, errors.New("飞书应用凭据不完整")
	}
	client := lark.NewClient(worker.record.AppID, secret)
	wsClient := newFeishuWSClient(worker.record.AppID, secret)
	requireMention := true
	respondToMentionAll := false
	safetyConfig := channeltypes.DefaultChannelConfig().Safety
	safetyConfig.Batch.MaxMessages = 1
	bot := channel.NewChannel(client, wsClient, channeltypes.WithPolicyConfig(channeltypes.PolicyConfig{
		RequireMention:      &requireMention,
		RespondToMentionAll: &respondToMentionAll,
		DMMode:              "open",
	}), channeltypes.WithSafetyConfig(safetyConfig))
	adapter := &feishuChannel{worker: worker, client: client, channel: bot, incoming: newFeishuInputPipelines(channeltypes.DefaultBatchConfig())}
	bot.OnReady(func() { worker.online.Store(true) })
	bot.OnReconnecting(func() { worker.online.Store(false) })
	bot.OnReconnected(func() { worker.online.Store(true) })
	bot.OnDisconnected(func() { worker.online.Store(false) })
	wsClient.EventHandler().OnP2MessageReceiveV1(adapter.receiveEvent)
	bot.OnCardAction(worker.handleCardAction)
	return adapter, nil
}

func (w *channelWorker) dispatchMessage(ctx context.Context, msg *channeltypes.NormalizedMessage) error {
	if msg == nil {
		return nil
	}
	normalized := *msg
	msg = &normalized
	if msg.RawContentType == "post" {
		if raw, ok := msg.RawEvent.(*larkim.P2MessageReceiveV1); ok && raw.Event != nil && raw.Event.Message != nil && raw.Event.Message.Content != nil {
			var body struct {
				Title     string          `json:"title"`
				Content   json.RawMessage `json:"content"`
				ContentV2 json.RawMessage `json:"content_v2"`
				Files     []struct {
					FileKey  string `json:"file_key"`
					FileName string `json:"file_name"`
					IsFolder bool   `json:"is_folder"`
				} `json:"files"`
			}
			content := *raw.Event.Message.Content
			if err := json.Unmarshal([]byte(content), &body); err != nil {
				return fmt.Errorf("解析飞书富文本失败: %w", err)
			}
			if len(body.Content) > 0 || len(body.ContentV2) > 0 {
				msg.Content, msg.Resources = normalize.ParseContent("post", `{"zh_cn":`+content+`}`)
			}
			if len(body.Files) > 0 && msg.Content == "[rich text message]" {
				msg.Content = body.Title
			}
			for _, file := range body.Files {
				kind := "file"
				if file.IsFolder {
					kind = "folder"
				}
				index := slices.IndexFunc(msg.Resources, func(resource channeltypes.Resource) bool {
					return resource.FileKey == file.FileKey && resource.Type != "image"
				})
				if index >= 0 {
					msg.Resources[index].Type = kind
					if msg.Resources[index].FileName == "" {
						msg.Resources[index].FileName = file.FileName
					}
					continue
				}
				msg.Resources = append(msg.Resources, channeltypes.Resource{Type: kind, FileKey: file.FileKey, FileName: file.FileName})
			}
		}
	}
	for _, mention := range msg.Mentions {
		if mention.IsBot && mention.Key != "" {
			msg.Content = strings.ReplaceAll(msg.Content, mention.Key, "")
		}
	}
	return w.receiveChannelMessage(ctx, feishuMessage(msg))
}

func (f *feishuChannel) queueInput(ctx context.Context, msg *channelMessage) error {
	w := f.worker
	scopeKey := msg.input.ScopeKey
	batchKey := scopeKey + "\x00" + msg.UserID
	w.mu.Lock()
	queue := w.scopes[scopeKey]
	busy := queue != nil && queue.held || len(w.replies[scopeKey]) > 0
	w.mu.Unlock()
	if !busy || len(msg.Resources) > 0 || strings.HasPrefix(msg.Content, "/") {
		return f.incoming.scope(batchKey).Run(w.baseContext(), func() error {
			return w.prepareInputMessage(ctx, msg, scopeKey)
		})
	}
	normalized := &channeltypes.NormalizedMessage{
		MessageID: msg.MessageID, ChatID: msg.ChatID, ChatType: msg.ChatType, UserID: msg.UserID,
		Content: msg.Content, RawContentType: msg.RawContentType,
		RawEvent: map[string]any{"root_id": msg.RootID, "thread_id": msg.ThreadID},
	}
	f.incoming.scope(batchKey).Push(w.baseContext(), normalized, func(ctx context.Context, batch *channeltypes.BatchedDispatch) error {
		message := feishuMessage(batch.Message)
		message.MessageIDs = batch.SourceIDs
		return w.prepareInputMessage(ctx, message, scopeKey)
	})
	return nil
}

func (f *feishuChannel) receiveEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	msg := normalize.ParseMessage(event)
	if msg == nil {
		return nil
	}
	identity := f.channel.GetBotIdentity(ctx)
	if identity != nil {
		if msg.UserID == identity.OpenID {
			return nil
		}
		for i := range msg.Mentions {
			mention := &msg.Mentions[i]
			if mention.OpenID == identity.OpenID || mention.UserID == identity.OpenID || identity.UserID != "" && mention.UserID == identity.UserID {
				msg.MentionedBot = true
				mention.IsBot = true
			}
		}
	}
	policy := f.channel.GetPolicy()
	if safety.IsStale(msg.CreateTimeMs, channeltypes.DefaultStaleWindow) || !safety.NewPolicyGate(&policy, identity).Evaluate(msg).Allowed {
		return nil
	}
	return f.worker.dispatchMessage(ctx, msg)
}
