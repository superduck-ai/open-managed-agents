package channels

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestChannelPreviewIsReplacedByFinalMessage(t *testing.T) {
	worker, card := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	runner := newReplyRunner(worker, &channelReplyTask{}, &channelMessage{})
	runner.stream = fakeStream{channel: card}
	for _, event := range []sessions.StreamEvent{
		{EventType: "event_start", Payload: []byte(`{"event":{"id":"message","type":"agent.message"}}`)},
		{EventType: "event_delta", Payload: []byte(`{"event_id":"message","delta":{"content":{"text":"中文"}}}`)},
		{EventType: "event_delta", Payload: []byte(`{"event_id":"message","delta":{"content":{"text":"🧑‍💻"}}}`)},
	} {
		if err := runner.consumePreview(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	if got := runner.reply.render(); strings.Count(got, "中文🧑‍💻") != 1 {
		t.Fatalf("增量未合并为同一个正文: %s", got)
	}
	if _, err := runner.consumeEvent(t.Context(), omaEvent{ID: "message", Type: "agent.message", Content: []byte(`"中文🧑‍💻最终正文"`)}); err != nil {
		t.Fatal(err)
	}
	got := runner.reply.render()
	if strings.Count(got, "中文🧑‍💻") != 1 || !strings.Contains(got, "最终正文") || runner.reply.text.String() != "中文🧑‍💻最终正文\n" {
		t.Fatalf("最终事件必须替换预览，不能重复拼接: %s", got)
	}
	runner.lastUpdate = time.Now().Add(-channelPreviewUpdateInterval)
	if err := runner.consumePreview(t.Context(), sessions.StreamEvent{EventType: "event_delta", Payload: []byte(`{"event_id":"message","delta":{"content":{"text":"迟到增量"}}}`)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(runner.reply.render(), "迟到增量") {
		t.Fatal("最终消息之后不能继续追加预览")
	}
}

func TestChannelPreviewResetPreservesDurableContent(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	runner := newReplyRunner(worker, &channelReplyTask{}, &channelMessage{})
	runner.reply.consume(db.ChannelRecord{}, omaEvent{ID: "durable", Type: "agent.message", Content: []byte(`"持久正文"`)})
	for _, event := range []sessions.StreamEvent{
		{EventType: "event_start", Payload: []byte(`{"event":{"id":"unfinished","type":"agent.message"}}`)},
		{EventType: "event_delta", Payload: []byte(`{"event_id":"unfinished","delta":{"content":{"text":"断线前的临时正文"}}}`)},
	} {
		if err := runner.consumePreview(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	runner.resetPreviews()
	got := runner.reply.render()
	if !strings.Contains(got, "持久正文") || strings.Contains(got, "临时正文") {
		t.Fatalf("断线只应丢弃临时预览: %s", got)
	}
}

func TestChannelPreviewHonorsThinkingConfiguration(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
		worker.record.IncludeThinking = enabled
		runner := newReplyRunner(worker, &channelReplyTask{}, &channelMessage{})
		if err := runner.consumePreview(t.Context(), sessions.StreamEvent{EventType: "event_start", Payload: []byte(`{"event":{"id":"thinking","type":"agent.thinking"}}`)}); err != nil {
			t.Fatal(err)
		}
		if runner.reply.thinking != enabled {
			t.Fatalf("思考展示未遵循配置: enabled=%v", enabled)
		}
	}
}
