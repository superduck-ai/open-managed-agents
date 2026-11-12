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

type replyRunner struct {
	worker    *channelWorker
	task      *channelReplyTask
	msg       *channeltypes.NormalizedMessage
	reply     channelReply
	stream    channeltypes.StreamController
	seen      map[string]struct{}
	started   bool
	cursor    int
	turnStart int
	progress  time.Time
}

func newReplyRunner(worker *channelWorker, task *channelReplyTask, msg *channeltypes.NormalizedMessage) *replyRunner {
	return &replyRunner{
		worker:   worker,
		task:     task,
		msg:      msg,
		reply:    channelReply{card: worker.record.IncludeThinking, thinking: worker.record.IncludeThinking},
		seen:     make(map[string]struct{}),
		started:  task.inputEventID == "",
		progress: time.Now(),
	}
}

func (w *channelWorker) streamReply(ctx context.Context, task *channelReplyTask, msg *channeltypes.NormalizedMessage, apiKey string) error {
	runner := newReplyRunner(w, task, msg)
	defer runner.close(ctx)
	if !task.listener {
		if err := runner.open(ctx); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(channelReplyTimeout)
	ticker := time.NewTicker(channelReplyPollInterval)
	defer ticker.Stop()
	for {
		if !w.sessionBound(ctx, task) {
			return nil
		}
		events, err := w.supervisor.oma.listEvents(ctx, apiKey, task.sessionID)
		if err == nil {
			done, err := runner.consume(ctx, events)
			if err != nil {
				return err
			}
			if done {
				return runner.close(ctx)
			}
			runner.askPending(ctx, events)
		}
		if task.idle > 0 && time.Since(runner.progress) > task.idle {
			return runner.close(ctx)
		}
		if time.Now().After(deadline) {
			return runner.close(ctx)
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

func (r *replyRunner) open(ctx context.Context) error {
	if r.stream != nil {
		return nil
	}
	opened, err := r.worker.openStream(ctx, r.msg)
	if err != nil {
		return err
	}
	r.stream = opened
	if r.reply.card {
		return r.stream.UpdateCard(ctx, r.reply.render())
	}
	return nil
}

func (r *replyRunner) close(ctx context.Context) error {
	if r.stream == nil {
		return nil
	}
	closing := r.stream
	r.stream = nil
	return closing.Close(context.WithoutCancel(ctx))
}

func (r *replyRunner) seek(events []omaEvent) {
	if r.started {
		return
	}
	for index, event := range events {
		if event.ID == r.task.inputEventID {
			r.started = true
			r.cursor = index + 1
			r.turnStart = index + 1
			return
		}
	}
}

func (r *replyRunner) consume(ctx context.Context, events []omaEvent) (bool, error) {
	r.seek(events)
	if !r.started {
		return false, nil
	}
	if r.cursor > len(events) {
		r.cursor = 0
	}
	done := false
	for ; r.cursor < len(events); r.cursor++ {
		event := events[r.cursor]
		if _, ok := r.seen[event.ID]; ok {
			continue
		}
		r.seen[event.ID] = struct{}{}
		r.progress = time.Now()
		r.worker.setSessionTail(r.task.sessionID, event.ID)
		if err := r.write(ctx, event); err != nil {
			return false, err
		}
		if terminalEvent(event) {
			done = true
		}
	}
	return done, nil
}

func (r *replyRunner) write(ctx context.Context, event omaEvent) error {
	text, changed := r.reply.consume(r.worker.record, event)
	if !changed {
		return nil
	}
	if err := r.open(ctx); err != nil {
		return err
	}
	if r.reply.card {
		return r.stream.UpdateCard(ctx, r.reply.render())
	}
	if text == "" {
		return nil
	}
	return r.stream.Append(ctx, text)
}

func (r *replyRunner) askPending(ctx context.Context, events []omaEvent) {
	if r.turnStart > len(events) {
		r.turnStart = len(events)
	}
	pending := events[r.turnStart:]
	answered := answeredToolIDs(pending)
	for _, event := range pending {
		if err := r.worker.askIfNeeded(ctx, r.msg, r.task, event, answered); err != nil {
			r.worker.supervisor.logger.WarnContext(ctx, "send channel action card", "channel_id", r.worker.record.ExternalID, "error", err)
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
