package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	"github.com/larksuite/oapi-sdk-go/v3/channel/safety"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
)

type Adapter struct {
	Config   Config
	Client   *lark.Client
	Channel  channeltypes.Channel
	Incoming *InputPipelines
}

func (f *Adapter) Start(ctx context.Context) error { return f.Channel.Start(ctx) }
func (f *Adapter) Close(ctx context.Context) error {
	f.Incoming.Dispose()
	return f.Channel.Stop(ctx)
}

func Message(msg *channeltypes.NormalizedMessage) *contracts.Message {
	if msg == nil {
		return nil
	}
	resources := make([]contracts.Attachment, len(msg.Resources))
	for i, r := range msg.Resources {
		resources[i] = contracts.Attachment{Type: r.Type, FileKey: r.FileKey, FileName: r.FileName}
	}
	rootID, threadID := "", ""
	if msg.ChatType != "p2p" {
		rootID = messageID(msg, []string{"root_id"})
		threadID = messageID(msg, []string{"thread_id", "root_id"})
	}
	return &contracts.Message{MessageID: msg.MessageID, MessageIDs: []string{msg.MessageID}, ChatID: msg.ChatID, ChatType: msg.ChatType, UserID: msg.UserID, Content: strings.TrimSpace(msg.Content), RawContentType: msg.RawContentType, RootID: rootID, ThreadID: threadID, Resources: resources}
}

func messageID(msg *channeltypes.NormalizedMessage, keys []string) string {
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

func newWSClient(appID, secret string) *larkws.Client {
	return larkws.NewClient(appID, secret, larkws.WithEventHandler(larkevent.NewEventDispatcher("", "")))
}

func New(config Config) (contracts.Adapter, error) {
	if config.AppID == "" || config.Secret == "" {
		return nil, errors.New("飞书应用凭据不完整")
	}
	client := lark.NewClient(config.AppID, config.Secret)
	wsClient := newWSClient(config.AppID, config.Secret)
	requireMention := true
	respondToMentionAll := false
	safetyConfig := channeltypes.DefaultChannelConfig().Safety
	safetyConfig.Batch.MaxMessages = 1
	bot := channel.NewChannel(client, wsClient, channeltypes.WithPolicyConfig(channeltypes.PolicyConfig{
		RequireMention:      &requireMention,
		RespondToMentionAll: &respondToMentionAll,
		DMMode:              "open",
	}), channeltypes.WithSafetyConfig(safetyConfig))
	adapter := &Adapter{Config: config, Client: client, Channel: bot, Incoming: NewInputPipelines(channeltypes.DefaultBatchConfig())}
	bot.OnReady(func() { config.Online(true) })
	bot.OnReconnecting(func() { config.Online(false) })
	bot.OnReconnected(func() { config.Online(true) })
	bot.OnDisconnected(func() { config.Online(false) })
	wsClient.EventHandler().OnP2MessageReceiveV1(adapter.ReceiveEvent)
	wsClient.EventHandler().OnP2BotMenuV6(adapter.receiveMenuEvent)
	bot.OnCardAction(func(ctx context.Context, event *channeltypes.CardActionEvent) error {
		return config.Action(ctx, NormalizeAction(event))
	})
	return adapter, nil
}

func (f *Adapter) receiveMenuEvent(ctx context.Context, event *larkapplication.P2BotMenuV6) error {
	if event.EventV2Base == nil {
		return nil
	}
	header := event.EventV2Base.Header
	if header == nil || header.AppID != f.Config.AppID || header.EventID == "" {
		return nil
	}
	menu := event.Event
	if menu == nil || menu.EventKey == nil || *menu.EventKey != "chat_new" || menu.Operator == nil || menu.Operator.OperatorId == nil {
		return nil
	}
	openID := menu.Operator.OperatorId.OpenId
	if openID == nil || *openID == "" {
		return nil
	}
	return f.Config.NewSession(ctx, header.EventID, *openID)
}

func NormalizeMessage(msg *channeltypes.NormalizedMessage) (*contracts.Message, error) {
	if msg == nil {
		return nil, nil
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
				return nil, fmt.Errorf("解析飞书富文本失败: %w", err)
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
	return Message(msg), nil
}

func (f *Adapter) QueueInput(ctx context.Context, msg *contracts.Message) error {
	scopeKey := msg.Input.ScopeKey
	batchKey := scopeKey + "\x00" + msg.UserID
	busy := f.Config.Busy(scopeKey)
	if !busy || len(msg.Resources) > 0 || strings.HasPrefix(msg.Content, "/") {
		return f.Incoming.scope(batchKey).Run(f.Config.Context(), func() error {
			return f.Config.Prepare(ctx, msg, scopeKey)
		})
	}
	normalized := &channeltypes.NormalizedMessage{
		MessageID: msg.MessageID, ChatID: msg.ChatID, ChatType: msg.ChatType, UserID: msg.UserID,
		Content: msg.Content, RawContentType: msg.RawContentType,
		RawEvent: map[string]any{"root_id": msg.RootID, "thread_id": msg.ThreadID},
	}
	f.Incoming.scope(batchKey).Push(f.Config.Context(), normalized, func(ctx context.Context, batch *channeltypes.BatchedDispatch) error {
		message := Message(batch.Message)
		message.MessageIDs = batch.SourceIDs
		return f.Config.Prepare(ctx, message, scopeKey)
	})
	return nil
}

func (f *Adapter) ReceiveEvent(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	msg := normalize.ParseMessage(event)
	if msg == nil {
		return nil
	}
	identity := f.Channel.GetBotIdentity(ctx)
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
	policy := f.Channel.GetPolicy()
	if safety.IsStale(msg.CreateTimeMs, channeltypes.DefaultStaleWindow) || !safety.NewPolicyGate(&policy, identity).Evaluate(msg).Allowed {
		return nil
	}
	message, err := NormalizeMessage(msg)
	if err != nil {
		return err
	}
	return f.Config.Receive(ctx, message)
}
