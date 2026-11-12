package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/channel"
	"github.com/larksuite/oapi-sdk-go/v3/channel/normalize"
	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (w *channelWorker) streamReply(ctx context.Context, task *channelReplyTask, msg *channeltypes.NormalizedMessage, apiKey string) error {
	stream, err := w.openStream(ctx, msg)
	if err != nil {
		return err
	}
	defer stream.Close(context.WithoutCancel(ctx))
	reply := channelReply{card: w.record.IncludeThinking, thinking: w.record.IncludeThinking}
	if reply.card {
		if err := stream.UpdateCard(ctx, reply.render()); err != nil {
			return err
		}
	}
	seen := make(map[string]struct{})
	started := false
	deadline := time.Now().Add(channelReplyTimeout)
	ticker := time.NewTicker(channelReplyPollInterval)
	defer ticker.Stop()
	for {
		events, err := w.supervisor.oma.listEvents(ctx, apiKey, task.sessionID)
		if err == nil {
			done := false
			answered := answeredToolIDs(events)
			for _, event := range events {
				if !started {
					if event.ID == task.inputEventID {
						started = true
					}
					continue
				}
				if _, ok := seen[event.ID]; ok {
					continue
				}
				seen[event.ID] = struct{}{}
				text, changed := reply.consume(w.record, event)
				if changed {
					if reply.card {
						if err := stream.UpdateCard(ctx, reply.render()); err != nil {
							return err
						}
					} else if text != "" {
						if err := stream.Append(ctx, text); err != nil {
							return err
						}
					}
				}
				if err := w.askIfNeeded(ctx, msg, task, event, answered); err != nil {
					w.supervisor.logger.WarnContext(ctx, "send channel action card", "channel_id", w.record.ExternalID, "error", err)
				}
				if terminalEvent(event) {
					done = true
				}
			}
			if done && started {
				return stream.Close(ctx)
			}
		}
		if time.Now().After(deadline) {
			return stream.Close(ctx)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.supervisor.shutdown:
			return nil
		case <-ticker.C:
		}
	}
}

func (w *channelWorker) openStream(ctx context.Context, msg *channeltypes.NormalizedMessage) (channeltypes.StreamController, error) {
	if !w.record.ReplyInThread || msg.MessageID == "" {
		if w.record.IncludeThinking {
			return w.channel.Stream(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Card: channelCardStart()})
		}
		return w.channel.Stream(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Markdown: "正在处理…"})
	}
	if w.record.IncludeThinking {
		messageID, err := w.replyMessage(ctx, msg.MessageID, "interactive", channelCardStart())
		if err != nil {
			return nil, err
		}
		return channel.NewCardStreamController(w.client, channeltypes.DefaultChannelConfig(), messageID), nil
	}
	content, err := normalize.SimpleMarkdownToPost("", "正在处理…", nil)
	if err != nil {
		return nil, err
	}
	messageID, err := w.replyMessage(ctx, msg.MessageID, "post", content)
	if err != nil {
		return nil, err
	}
	return channel.NewMarkdownStreamController(w.client, channeltypes.DefaultChannelConfig(), messageID, "正在处理…", ""), nil
}

func (w *channelWorker) replyMessage(ctx context.Context, messageID, msgType, content string) (string, error) {
	request := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType(msgType).Content(content).ReplyInThread(true).Build()).
		Build()
	response, err := w.client.Im.V1.Message.Reply(ctx, request)
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", fmt.Errorf("飞书回复消息失败: %d %s", response.Code, response.Msg)
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", errors.New("飞书回复消息缺少 message_id")
	}
	return *response.Data.MessageId, nil
}

func (w *channelWorker) replyText(ctx context.Context, msg *channeltypes.NormalizedMessage, text string) error {
	if w.record.ReplyInThread && msg.MessageID != "" {
		content, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return err
		}
		_, err = w.replyMessage(ctx, msg.MessageID, "text", string(content))
		return err
	}
	_, err := w.channel.Send(ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Text: text})
	return err
}

type channelReply struct {
	card     bool
	thinking bool
	text     strings.Builder
}

func (r *channelReply) consume(record db.ChannelRecord, event omaEvent) (string, bool) {
	switch event.Type {
	case "agent.message":
		text := eventText(event)
		if text == "" {
			return "", false
		}
		r.text.WriteString(text + "\n")
		r.thinking = false
		return text + "\n", true
	case "agent.thinking":
		if record.IncludeThinking && !r.thinking {
			r.thinking = true
			return "", true
		}
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		if !record.IncludeToolCalls {
			return "", false
		}
		name := eventToolName(event)
		if name == "" {
			return "", false
		}
		line := "🔧 " + name + "\n"
		r.text.WriteString(line)
		r.thinking = false
		return line, true
	}
	return "", false
}

func (r *channelReply) render() string {
	return channelCard(r.text.String(), r.thinking)
}

func channelCardStart() string {
	return channelCard("", true)
}

func channelCard(text string, thinking bool) string {
	elements := make([]map[string]any, 0, 2)
	if text != "" {
		elements = append(elements, map[string]any{"tag": "markdown", "content": text})
	}
	if thinking {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_🧠 正在思考…_", "text_size": "notation"})
	}
	if len(elements) == 0 {
		elements = append(elements, map[string]any{"tag": "markdown", "content": "_正在处理…_"})
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{"update_multi": true},
		"body":   map[string]any{"elements": elements},
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func terminalEvent(event omaEvent) bool {
	switch event.Type {
	case "session.status_idle", "session.thread_status_idle":
		reason := stopReason(event)
		return reason == "end_turn" || reason == "interrupted" || reason == "retries_exhausted"
	case "session.status_terminated", "session.error":
		return true
	default:
		return false
	}
}
