//go:build integration || e2e || e2b_integration

package tests

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestSessionRunningInterval(t *testing.T) {
	t.Run("failure early close drops the open interval", func(t *testing.T) {
		app := newPayloadIntegrationApp(t, newFakeStore("running-early"))
		worker, _ := newPayloadIntegrationSession(t, app)
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_running","id":"sevt_early_running","processed_at":"2026-06-16T04:00:00Z"}`,
			`{"type":"session.status_idle","id":"sevt_early_idle","processed_at":"2026-06-16T03:59:00Z","stop_reason":{"type":"end_turn"}}`,
		)
		if _, ok := sessionActiveMilliseconds(t, app, worker); ok {
			t.Fatal("early idle stored active_milliseconds")
		}
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_running","id":"sevt_early_running_again","processed_at":"2026-06-16T05:00:00Z"}`,
			`{"type":"session.status_idle","id":"sevt_early_idle_again","processed_at":"2026-06-16T05:00:10Z","stop_reason":{"type":"end_turn"}}`,
		)
		if got := mustSessionActiveMilliseconds(t, app, worker); got != 10000 {
			t.Fatalf("active_milliseconds = %v, want 10000", got)
		}
	})

	t.Run("failure replay does not add the interval twice", func(t *testing.T) {
		app := newPayloadIntegrationApp(t, newFakeStore("running-replay"))
		worker, _ := newPayloadIntegrationSession(t, app)
		payloads := []string{
			`{"type":"session.status_running","id":"sevt_replay_running","processed_at":"2026-06-16T01:00:00Z"}`,
			`{"type":"session.status_idle","id":"sevt_replay_idle","processed_at":"2026-06-16T01:01:30Z","stop_reason":{"type":"end_turn"}}`,
		}
		publishSessionPayloads(t, app, worker, payloads...)
		publishSessionPayloads(t, app, worker, payloads...)
		if got := mustSessionActiveMilliseconds(t, app, worker); got != 90000 {
			t.Fatalf("active_milliseconds = %v, want 90000", got)
		}
		if n := len(listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.usage", defaultTestKey).Data); n != 1 {
			t.Fatalf("usage snapshots = %d, want 1", n)
		}
	})

	t.Run("failure another running thread does not close the session interval", func(t *testing.T) {
		app := newPayloadIntegrationApp(t, newFakeStore("running-fold"))
		worker, epoch := newPayloadIntegrationSession(t, app)
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_running","id":"sevt_fold_running","processed_at":"2026-06-16T01:00:00Z"}`,
		)
		postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
			`{"type":"system","uuid":"fold-child","subtype":"task_started","task_id":"task-fold","tool_use_id":"tool-fold","description":"Child"}`))
		threads, err := app.db.ListSessionThreads(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
		if err != nil {
			t.Fatal(err)
		}
		var childID string
		for _, thread := range threads {
			if thread.ParentThreadUUID != nil {
				childID = thread.ExternalID
			}
		}
		if childID == "" {
			t.Fatal("child thread missing")
		}
		publishSessionPayloads(t, app, worker,
			`{"type":"session.thread_status_idle","id":"sevt_fold_child_idle","processed_at":"2026-06-16T01:00:02Z","session_thread_id":"`+childID+`","stop_reason":{"type":"end_turn"}}`,
		)
		if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "running" {
			t.Fatalf("session status = %s, want running", status)
		}
		if _, ok := sessionActiveMilliseconds(t, app, worker); ok {
			t.Fatal("child idle closed the session interval")
		}
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_idle","id":"sevt_fold_idle","processed_at":"2026-06-16T01:00:10Z","stop_reason":{"type":"end_turn"}}`,
		)
		if got := mustSessionActiveMilliseconds(t, app, worker); got != 10000 {
			t.Fatalf("active_milliseconds = %v, want 10000", got)
		}
	})

	t.Run("rescheduling closes and the next running interval starts over", func(t *testing.T) {
		app := newPayloadIntegrationApp(t, newFakeStore("running-reschedule"))
		worker, _ := newPayloadIntegrationSession(t, app)
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_running","id":"sevt_reschedule_running","processed_at":"2026-06-16T01:00:00Z"}`,
			`{"type":"session.status_rescheduled","id":"sevt_reschedule","processed_at":"2026-06-16T01:00:05Z"}`,
		)
		if got := mustSessionActiveMilliseconds(t, app, worker); got != 5000 {
			t.Fatalf("active_milliseconds after reschedule = %v, want 5000", got)
		}
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_running","id":"sevt_reschedule_running_again","processed_at":"2026-06-16T01:01:40Z"}`,
			`{"type":"session.status_idle","id":"sevt_reschedule_idle","processed_at":"2026-06-16T01:01:50Z","stop_reason":{"type":"end_turn"}}`,
		)
		if got := mustSessionActiveMilliseconds(t, app, worker); got != 15000 {
			t.Fatalf("active_milliseconds = %v, want 15000", got)
		}
		usageEvent := sessionUsageObject(t, app, worker.SessionExternalID)
		if got, ok := nestedActiveMilliseconds(t, usageEvent); !ok || got != 15000 {
			t.Fatalf("usage snapshot active_milliseconds = %v, %v, want 15000", got, ok)
		}
	})

	t.Run("success idle snapshot includes active seconds", func(t *testing.T) {
		app := newPayloadIntegrationApp(t, newFakeStore("running-success"))
		rate, err := config.RuntimeCostConfig{USDPerHour: "0.01652866"}.HourlyUSD()
		if err != nil {
			t.Fatal(err)
		}
		app.db.SetRuntimeUSDPerHour(rate)
		worker, _ := newPayloadIntegrationSession(t, app)
		publishSessionPayloads(t, app, worker,
			`{"type":"session.status_running","id":"sevt_success_running","processed_at":"2026-06-16T01:00:00Z"}`,
			`{"type":"span.model_request_start","id":"sevt_success_start","processed_at":"2026-06-16T01:00:00.100Z"}`,
			`{"type":"span.model_request_end","id":"sevt_success_end","processed_at":"2026-06-16T01:00:01Z","model_request_start_id":"sevt_success_start","is_error":false,"model_usage":{"input_tokens":5,"output_tokens":0,"cache_creation_input_tokens":3,"cache_read_input_tokens":2,"cache_creation":{"ephemeral_5m_input_tokens":3}}}`,
			`{"type":"session.status_idle","id":"sevt_success_idle","processed_at":"2026-06-16T01:00:01.500Z","stop_reason":{"type":"end_turn"}}`,
		)
		current, _, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := usageActiveMilliseconds(t, current.Usage); !ok || got != 1500 {
			t.Fatalf("session active_milliseconds = %v, %v, want 1500 from %s", got, ok, current.Usage)
		}
		var tokens struct {
			Input int `json:"input_tokens"`
		}
		if err := json.Unmarshal(current.Usage, &tokens); err != nil || tokens.Input != 5 {
			t.Fatalf("token usage = %s, %v", current.Usage, err)
		}
		primary, _, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := usageActiveMilliseconds(t, primary.Usage); ok {
			t.Fatalf("thread usage includes active_milliseconds: %s", primary.Usage)
		}
		usageEvent := sessionUsageObject(t, app, worker.SessionExternalID)
		if got, ok := nestedActiveMilliseconds(t, usageEvent); !ok || got != 1500 {
			t.Fatalf("usage snapshot = %s", usageEvent)
		}
		cost, err := app.db.RuntimeCost(t.Context(), db.RuntimeCostQuery{
			OrganizationUUID: worker.OrganizationUUID,
			GroupBy:          db.RuntimeCostGroupModel,
			StartingAt:       time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC),
			EndingBefore:     time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC),
			USDPerHour:       rate,
		})
		if err != nil {
			t.Fatal(err)
		}
		if cost.Total != "0.000007" || len(cost.Daily) != 1 || cost.Daily[0].Amount != "0.000007" {
			t.Fatalf("runtime cost = %+v", cost)
		}
	})
}

func publishSessionPayloads(t *testing.T, app *testApp, worker db.CodeSession, payloads ...string) {
	t.Helper()
	sink := sessionsapi.NewHandler(app.cfg, app.db, newCodeSessionService(app, nil, nil), nil, nil, app.vaultSecrets, nil)
	raws := make([]json.RawMessage, len(payloads))
	for i, payload := range payloads {
		raws[i] = json.RawMessage(payload)
	}
	if err := sink.PublishCodeSessionEvents(t.Context(), worker, raws); err != nil {
		t.Fatal(err)
	}
}

func sessionActiveMilliseconds(t *testing.T, app *testApp, worker db.CodeSession) (int64, bool) {
	t.Helper()
	current, _, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	return usageActiveMilliseconds(t, current.Usage)
}

func mustSessionActiveMilliseconds(t *testing.T, app *testApp, worker db.CodeSession) int64 {
	t.Helper()
	got, ok := sessionActiveMilliseconds(t, app, worker)
	if !ok {
		t.Fatal("active_milliseconds missing")
	}
	return got
}

func usageActiveMilliseconds(t *testing.T, raw json.RawMessage) (int64, bool) {
	t.Helper()
	var usage struct {
		Active *int64 `json:"session_active_milliseconds"`
	}
	if err := json.Unmarshal(raw, &usage); err != nil {
		t.Fatal(err)
	}
	if usage.Active == nil {
		return 0, false
	}
	return *usage.Active, true
}

func sessionUsageObject(t *testing.T, app *testApp, sessionID string) json.RawMessage {
	t.Helper()
	records := listSessionEvents(t, app, sessionID, "types[]=session.usage&order=asc", defaultTestKey)
	if len(records.Data) == 0 {
		t.Fatal("session.usage missing")
	}
	return records.Data[len(records.Data)-1]
}

func nestedActiveMilliseconds(t *testing.T, event json.RawMessage) (int64, bool) {
	t.Helper()
	var body struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(event, &body); err != nil {
		t.Fatal(err)
	}
	return usageActiveMilliseconds(t, body.Usage)
}
