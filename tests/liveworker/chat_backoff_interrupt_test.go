package liveworker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func verifyBackoffInterrupt(t *testing.T, e *liveEnv) {
	t.Helper()
	var calls, replies atomic.Int32
	var failing atomic.Bool
	failing.Store(true)
	normal := sequentialChatModel(t, nil, &replies, 0)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		calls.Add(1)
		if !failing.Load() {
			normal.Config.Handler.ServeHTTP(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"Injected transient failure"}}`)
	}))
	t.Cleanup(model.Close)
	configureChatModel(t, e, model.URL)
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	started := time.Now()
	submitChat(t, f, "Reply with a short greeting.")
	waitRealWorker(t, "backoff Code Session created", func() bool {
		var err error
		f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
		if errors.Is(err, db.ErrNotFound) {
			return false
		}
		requireOK(t, err)
		return f.code.Status == "active"
	})
	registerPublicSandboxCleanup(t, e, f)
	defer func() {
		state := readUpstream401State(t, f, &calls, started)
		if state.SessionStatus == "running" {
			cleanupCtx, cleanupCancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cleanupCancel()
			sendBackoffInterrupt(t, f, cleanupCtx)
			state = waitUpstream401State(t, f, &calls, started, 15*time.Second, func(state upstream401State) bool {
				return state.SessionStatus == "idle"
			})
			if state.SessionStatus != "idle" {
				t.Errorf("backoff interrupt cleanup did not reach idle: %+v", state)
			}
		}
	}()
	before := waitUpstream401State(t, f, &calls, started, max(0, 60*time.Second-time.Since(started)), func(state upstream401State) bool {
		return state.ModelRequests >= 6 && state.ModelStarts == int(state.ModelRequests) && state.ModelEnds == state.ModelStarts
	})
	if before.ModelRequests < 6 || before.ModelEnds != int(before.ModelRequests) || before.SessionStatus != "running" || len(before.SessionErrorIDs) != 0 {
		t.Fatalf("did not observe completed failures before backoff interrupt: %+v", before)
	}
	interruptStarted := time.Now()
	interruptCtx, interruptCancel := context.WithTimeout(ctx, 15*time.Second)
	defer interruptCancel()
	sendBackoffInterrupt(t, f, interruptCtx)
	idle := nextChatEvent(t, interruptCtx, events, "session.status_idle")
	if idle.StopReason.Type != "end_turn" {
		t.Fatalf("backoff interrupted SSE stop reason = %s", idle.StopReason.Type)
	}
	after := waitUpstream401State(t, f, &calls, started, max(0, 15*time.Second-time.Since(interruptStarted)), func(state upstream401State) bool {
		return state.SessionStatus == "idle" && state.QueueReady && state.Pending == 0 && state.AckPending == 0
	})
	if time.Since(interruptStarted) > 15*time.Second || after.SessionStatus != "idle" || after.IdleStopReason != "end_turn" || len(after.SessionErrorIDs) != 0 ||
		after.ModelRequests != before.ModelRequests || after.Pending != 0 || after.AckPending != 0 || !after.QueueReady {
		t.Fatalf("backoff interrupt did not stop requests and finish cleanly: %+v", after)
	}
	assertBackoffIdleHistory(t, f, idle)
	failing.Store(false)
	submitChat(t, f, "Reply after interruption.")
	answer := nextChatEvent(t, ctx, events, "agent.message")
	if len(answer.Content) != 1 || answer.Content[0].Text != "回复1" {
		t.Fatalf("unexpected recovered answer: %+v", answer)
	}
	recoveredIdle := nextChatEvent(t, ctx, events, "session.status_idle")
	waitChatIdle(t, f, 1)
	assertBackoffIdleHistory(t, f, recoveredIdle)
	if calls.Load() != before.ModelRequests+1 || replies.Load() != 1 {
		t.Fatalf("unexpected requests after recovery: total=%d replies=%d", calls.Load(), replies.Load())
	}
	data, err := json.MarshalIndent(struct {
		Before upstream401State `json:"before_interrupt"`
		After  upstream401State `json:"after_interrupt"`
	}{before, after}, "", "  ")
	requireOK(t, err)
	path := filepath.Join(filepath.Dir(os.Getenv("VERIFY_BE_SERVER")), "upstream-backoff-observations.json")
	requireOK(t, os.WriteFile(path, append(data, '\n'), 0o600))
	t.Logf("backoff observations: %s", path)
}

func sendBackoffInterrupt(t *testing.T, f *liveSession, ctx context.Context) {
	t.Helper()
	f.env.requestContext(ctx, t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", f.env.apiKey,
		map[string]any{"events": []any{map[string]string{"type": "user.interrupt"}}}, http.StatusOK)
}

func assertBackoffIdleHistory(t *testing.T, f *liveSession, idle chatEvent) {
	t.Helper()
	found := false
	for _, event := range chatHistory(t, f) {
		if event.Type == "session.error" {
			t.Fatalf("unexpected session.error after backoff interrupt: %s", event.ID)
		}
		if event.ID == idle.ID {
			found = event.Type == "session.status_idle" && event.StopReason.Type == "end_turn" && idle.StopReason.Type == "end_turn"
		}
	}
	if !found {
		t.Fatal("SSE idle ID and end_turn missing from history")
	}
}
