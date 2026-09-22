package tests

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestSessionContractUsageIsAtomicAndIdempotent(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("contract-usage"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	sink := sessionsapi.NewHandler(app.cfg, app.db, newCodeSessionService(app, nil, nil), nil, nil, app.vaultSecrets, nil)
	publish := func(payloads ...string) {
		t.Helper()
		raws := make([]json.RawMessage, len(payloads))
		for i, payload := range payloads {
			raws[i] = json.RawMessage(strings.ReplaceAll(payload, "sevt_", "sevt_"+worker.ExternalID+"_"))
		}
		if err := sink.PublishCodeSessionEvents(t.Context(), worker, raws); err != nil {
			t.Fatal(err)
		}
	}
	batch := []string{
		`{"type":"session.status_running","id":"sevt_usage_running"}`,
		`{"type":"span.model_request_start","id":"sevt_usage_start"}`,
		`{"type":"span.model_request_end","id":"sevt_usage_end","model_request_start_id":"sevt_usage_start","is_error":false,"model_usage":{"input_tokens":5,"output_tokens":0,"cache_creation_input_tokens":3,"cache_read_input_tokens":2,"cache_creation":{"ephemeral_5m_input_tokens":3}}}`,
		`{"type":"session.status_idle","id":"sevt_usage_idle","stop_reason":{"type":"end_turn"}}`,
	}
	publish(batch...)
	publish(batch...)
	records := listSessionEvents(t, app, worker.SessionExternalID, "order=asc&limit=100", defaultTestKey)
	var usages int
	for i, raw := range records.Data {
		if sessionEventStringField(t, raw, "type") != "session.usage" {
			continue
		}
		usages++
		var event struct {
			Usage struct {
				Input  int  `json:"input_tokens"`
				Output *int `json:"output_tokens"`
				Cache  struct {
					Five int `json:"ephemeral_5m_input_tokens"`
				} `json:"cache_creation"`
			} `json:"usage"`
			Budget json.RawMessage `json:"budget"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.Usage.Input != 5 || event.Usage.Output == nil || *event.Usage.Output != 0 || event.Usage.Cache.Five != 3 || string(event.Budget) != "null" {
			t.Fatalf("incorrect usage snapshot: %s", raw)
		}
		if i+1 >= len(records.Data) || sessionEventStringField(t, records.Data[i+1], "type") != "session.status_idle" {
			t.Fatal("usage must immediately precede idle")
		}
	}
	if usages != 1 {
		t.Fatalf("replay duplicated usage snapshots: %d", usages)
	}
	current, _, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	var usage struct {
		Input int `json:"input_tokens"`
	}
	if err := json.Unmarshal(current.Usage, &usage); err != nil || usage.Input != 5 {
		t.Fatalf("replay counted usage twice: %s, %v", current.Usage, err)
	}
	primary, _, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(primary.Usage, &usage); err != nil || usage.Input != 5 {
		t.Fatalf("thread usage: %s, %v", primary.Usage, err)
	}
	// Waiting child cannot idle a still-running primary thread.
	publish(`{"type":"session.status_running","id":"sevt_primary_again"}`)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"system","uuid":"contract-child","subtype":"task_started","task_id":"task-contract","tool_use_id":"tool-contract","description":"Child"}`))
	threads, err := app.db.ListSessionThreads(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	var child db.SessionThread
	for _, thread := range threads {
		if thread.ParentThreadUUID != nil {
			child = thread
		}
	}
	if child.ExternalID == "" {
		t.Fatal("child thread missing")
	}
	publish(fmt.Sprintf(`{"type":"session.thread_status_idle","id":"sevt_child_wait","session_thread_id":%q,"stop_reason":{"type":"end_turn"}}`, child.ExternalID))
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("waiting child stopped parent: %s", status)
	}
	if n := len(listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.usage", defaultTestKey).Data); n != 1 {
		t.Fatalf("child wait emitted session usage while parent running: %d", n)
	}
	publish(`{"type":"session.status_idle","id":"sevt_parent_done","stop_reason":{"type":"end_turn"}}`)
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("all threads idle: %s", status)
	}
}
