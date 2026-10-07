package liveworker

import (
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

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const upstream401Budget = 90 * time.Second

type upstream401State struct {
	ElapsedMS       int64    `json:"elapsed_ms"`
	SessionStatus   string   `json:"session_status"`
	WorkerStatus    string   `json:"worker_status"`
	ModelRequests   int32    `json:"model_requests"`
	ModelStarts     int      `json:"model_starts"`
	ModelEnds       int      `json:"model_ends"`
	AgentMessages   int      `json:"agent_messages"`
	RetryStatus     string   `json:"retry_status"`
	IdleStopReason  string   `json:"idle_stop_reason"`
	SessionErrorIDs []string `json:"session_error_ids"`
	Pending         uint64   `json:"pending"`
	AckPending      int      `json:"ack_pending"`
	QueueReady      bool     `json:"queue_ready"`
}

type upstream401Observation struct {
	Case                      string            `json:"case"`
	NaturallyCompleted        bool              `json:"naturally_completed"`
	BeforeInterrupt           upstream401State  `json:"before_interrupt"`
	PostInterruptObservations *upstream401State `json:"post_interrupt_observations,omitempty"`
}

func (s upstream401State) failedAndIdle() bool {
	return s.ElapsedMS <= upstream401Budget.Milliseconds() && s.ModelRequests > 0 && s.ModelStarts == int(s.ModelRequests) && s.ModelEnds == int(s.ModelRequests) &&
		len(s.SessionErrorIDs) > 0 && s.AgentMessages == 0 && s.RetryStatus == "exhausted" && s.IdleStopReason == "retries_exhausted" &&
		s.SessionStatus == "idle" && s.QueueReady && s.Pending == 0 && s.AckPending == 0
}

func TestChatUpstreamErrors(t *testing.T) {
	if os.Getenv("VERIFY_BE_RUN_ID") == "" || os.Getenv("LIVE_WORKER_REAL_CLAUDE") != "1" {
		t.Skip("run through verify-be with isolated dependencies and a real Worker image")
	}
	started := time.Now()
	observations := make([]upstream401Observation, 0, 2)
	t.Cleanup(func() {
		data, err := json.MarshalIndent(observations, "", "  ")
		requireOK(t, err)
		path := filepath.Join(filepath.Dir(os.Getenv("VERIFY_BE_SERVER")), "upstream-401-observations.json")
		requireOK(t, os.WriteFile(path, append(data, '\n'), 0o600))
		t.Logf("upstream 401 observations: %s", path)
	})
	e := newLiveEnv(t)
	_, stopRunner := startPublicRunner(t, e)
	defer stopRunner()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"anthropic_401", `{"error":{"message":"invalid x-api-key","type":"authentication_error"},"request_id":"req_llm-mock","type":"error"}`},
		{"generic_401", `{"error":{"message":"Invalid fixture API key","type":"invalid_api_key","code":"invalid_api_key"}}`},
	} {
		if t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			model := upstream401Fixture(t, tc.body, &calls)
			configureChatModel(t, e, model.URL)
			f := createPublicChat(t, e)
			observation := upstream401Observation{Case: tc.name}
			turnStarted := time.Now()
			defer func() {
				observations = append(observations, observation)
				data, err := json.Marshal(observation)
				requireOK(t, err)
				t.Logf("UPSTREAM_401_OBSERVATION %s", data)
			}()
			defer func() {
				if observation.BeforeInterrupt.SessionStatus == "running" {
					e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey,
						map[string]any{"events": []any{map[string]string{"type": "user.interrupt"}}}, http.StatusOK)
					post := waitUpstream401State(t, f, &calls, turnStarted, 15*time.Second, func(state upstream401State) bool {
						return state.SessionStatus == "idle"
					})
					observation.PostInterruptObservations = &post
					if post.SessionStatus != "idle" {
						t.Errorf("public interrupt cleanup did not reach idle: %+v", post)
					}
				}
			}()
			submitChat(t, f, "Reply with a short greeting.")
			waitRealWorker(t, "public upstream error Code Session created", func() bool {
				var err error
				f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
				if errors.Is(err, db.ErrNotFound) {
					return false
				}
				requireOK(t, err)
				return f.code.Status == "active"
			})
			registerPublicSandboxCleanup(t, e, f)
			observation.BeforeInterrupt = waitUpstream401State(t, f, &calls, turnStarted, max(0, upstream401Budget-time.Since(turnStarted)), upstream401State.failedAndIdle)
			state := observation.BeforeInterrupt
			observation.NaturallyCompleted = state.failedAndIdle()
			if !observation.NaturallyCompleted {
				t.Errorf("BE_TIMEOUT upstream 401 did not report failure and finish within 90s: %+v", state)
			}
		}) {
			t.Logf("BE_PROOF {\"stage\":%q,\"elapsed_ms\":%d}", tc.name+"_failed_and_idle", time.Since(started).Milliseconds())
		}
	}
	if t.Run("backoff_500_interrupt", func(t *testing.T) { verifyBackoffInterrupt(t, e) }) {
		chatProof(t, started, "backoff_500_interrupted_and_recovered")
	}
}

func upstream401Fixture(t *testing.T, body string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func waitUpstream401State(t *testing.T, f *liveSession, calls *atomic.Int32, started time.Time, timeout time.Duration, complete func(upstream401State) bool) upstream401State {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		state := readUpstream401State(t, f, calls, started)
		if complete(state) || !time.Now().Before(deadline) {
			return state
		}
		timer := time.NewTimer(min(250*time.Millisecond, time.Until(deadline)))
		select {
		case <-t.Context().Done():
			timer.Stop()
			return state
		case <-timer.C:
		}
	}
}

func readUpstream401State(t *testing.T, f *liveSession, calls *atomic.Int32, started time.Time) upstream401State {
	t.Helper()
	var session struct {
		Status string `json:"status"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID, f.env.apiKey, nil, http.StatusOK), &session))
	code, found, err := f.env.database.GetCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	if !found {
		t.Fatal("upstream error Code Session disappeared")
	}
	state := upstream401State{SessionStatus: session.Status,
		WorkerStatus: code.WorkerStatus, ModelRequests: calls.Load(),
		SessionErrorIDs: []string{}}
	consumer, err := f.env.stream.Consumer(t.Context(), "oma_worker_"+f.code.ExternalID)
	if !errors.Is(err, jetstream.ErrConsumerNotFound) {
		requireOK(t, err)
		queue, err := consumer.Info(t.Context())
		requireOK(t, err)
		state.QueueReady, state.Pending, state.AckPending = true, queue.NumPending, queue.NumAckPending
	}
	for _, event := range chatHistory(t, f) {
		switch event.Type {
		case "session.error":
			state.SessionErrorIDs = append(state.SessionErrorIDs, event.ID)
			state.RetryStatus = event.Error.RetryStatus.Type
		case "session.status_idle":
			state.IdleStopReason = event.StopReason.Type
		case "agent.message":
			state.AgentMessages++
		case "span.model_request_start":
			state.ModelStarts++
		case "span.model_request_end":
			state.ModelEnds++
		}
	}
	state.ElapsedMS = time.Since(started).Milliseconds()
	return state
}
