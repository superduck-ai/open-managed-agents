package liveworker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestChatPerformance(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	var calls atomic.Int32
	model := sequentialChatModel(t, nil, &calls, 200*time.Millisecond)
	configureChatModel(t, e, model.URL)
	f := e.newSessionWithSnapshot(t, json.RawMessage(`{"model":{"id":"claude-sonnet-4-6"}}`))
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 120*time.Second))
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	startRealControlWorker(t, f, "")
	const warmup = 2
	const samples = 20
	var inputs []string
	for i := 0; i < warmup+samples; i++ {
		if i == warmup {
			if path := os.Getenv("VERIFY_BE_PROFILE_READY"); path != "" {
				requireOK(t, os.WriteFile(path, []byte("ready"), 0o600))
			}
		}
		begin := time.Now()
		inputs = append(inputs, submitChat(t, f, fmt.Sprintf("固定负载消息 %02d，请回复。", i)))
		accepted := time.Since(begin)
		preview := nextChatEvent(t, ctx, events, "event_delta")
		first := time.Since(begin)
		final := nextChatEvent(t, ctx, events, "agent.message")
		completed := time.Since(begin)
		if preview.EventID != final.ID || len(final.Content) != 1 || final.Content[0].Text != fmt.Sprintf("回复%d", i+1) {
			t.Fatal("performance turn response mismatch")
		}
		waitChatIdle(t, f, i+1)
		settled := time.Since(begin)
		if i >= warmup {
			sample := struct {
				Index  int     `json:"index"`
				Accept float64 `json:"accept_ms"`
				First  float64 `json:"first_preview_ms"`
				Final  float64 `json:"final_ms"`
				Idle   float64 `json:"idle_ms"`
			}{Index: i - warmup, Accept: float64(accepted) / float64(time.Millisecond), First: float64(first) / float64(time.Millisecond), Final: float64(completed) / float64(time.Millisecond), Idle: float64(settled) / float64(time.Millisecond)}
			encoded, err := json.Marshal(sample)
			requireOK(t, err)
			t.Logf("CHAT_SAMPLE %s", encoded)
		}
	}
	assertChatTurns(t, f, inputs)
	if calls.Load() != warmup+samples {
		t.Fatal("performance load retried or lost a model call")
	}
	chatProof(t, started, "fixed_load_completed")
	chatProof(t, started, "performance_history_verified")
}
