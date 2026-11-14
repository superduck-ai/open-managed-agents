package channels

import (
	"testing"
)

func TestReplyEventTrackingSuppressesRepeatedDelivery(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	task := &channelReplyTask{scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "input"}
	runner := newReplyRunner(worker, task, &channelMessage{MessageID: "message", ChatID: "oc_1", ChatType: "p2p", UserID: "actor"})
	first := omaEvent{ID: "first", Type: "agent.message", Content: []byte(`"第一段"`)}
	second := omaEvent{ID: "second", Type: "agent.message", Content: []byte(`"第二段"`)}
	if _, err := runner.consumeEvent(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.consumeEvent(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.consumeEvent(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if runner.reply.text.String() != "第一段\n第二段\n" {
		t.Fatalf("事件重排后丢失或重复输出: %q", runner.reply.text.String())
	}
}

func TestQueuedRepliesStopAtNextProcessedInput(t *testing.T) {
	worker, _ := testWorker(t, &fakeRuntimeStore{}, "http://unused")
	task := &channelReplyTask{scopeKey: "chat:oc_1", sessionID: "sesn_1", inputEventID: "first_input"}
	runner := newReplyRunner(worker, task, &channelMessage{MessageID: "message", ChatID: "oc_1", ChatType: "p2p", UserID: "actor"})
	second := omaEvent{ID: "second_input", Type: "user.message", ProcessedAt: []byte(`null`), Metadata: metadataJSON(t, channelInputMetadata{Type: "feishu", ChannelID: "chnl_1", AccountID: "app_1", ChatID: "oc_1", MessageID: "second_message", ActorID: "another_actor"})}
	answer := omaEvent{ID: "first_answer", Type: "agent.message", Content: []byte(`"queue_first_935"`)}
	if done, err := runner.consumeEvent(t.Context(), second); err != nil || done {
		t.Fatalf("待处理输入不能提前结束前轮: done=%v err=%v", done, err)
	}
	if _, err := runner.consumeEvent(t.Context(), answer); err != nil {
		t.Fatal(err)
	}
	second.ProcessedAt = []byte(`"2026-11-13T00:55:30Z"`)
	done, err := runner.consumeEvent(t.Context(), second)
	if err != nil || !done || !task.terminal || !task.normalEnd || runner.reply.status != "已完成" || runner.reply.text.String() != "queue_first_935\n" {
		t.Fatalf("连续轮次不能串入后轮回复: done=%v err=%v terminal=%v normal=%v status=%q reply=%q", done, err, task.terminal, task.normalEnd, runner.reply.status, runner.reply.text.String())
	}
}
