package tests

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestWorkerStateCommitsWithPublicStatus(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "worker-state-transaction", `{"model":"claude-opus-4-6","name":"worker-state-transaction"}`, `{"name":"worker-state-transaction"}`)
	for _, stage := range []string{"public", "private"} {
		t.Run(stage, func(t *testing.T) {
			response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			session := mustSessionRecord(t, app, response.ID)
			codeSessionID := launchLocalCodeSession(t, app, response.ID)
			epoch := registerCodeSessionWorker(t, app, codeSessionID)
			putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running","external_metadata":{"keep":9007199254740993}}`)
			before, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil {
				t.Fatal(err)
			}
			history := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
			watermark, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil {
				t.Fatal(err)
			}
			var removeFailure func()
			if stage == "public" {
				removeFailure = rejectPublicSessionEventWrites(t, app, session.UUID, "session.status_idle")
			} else {
				removeFailure = rejectSessionInputCommit(t, app, before.UUID, "metadata")
			}
			defer removeFailure()
			body := `{"worker_epoch":` + epoch + `,"worker_status":"requires_action","requires_action_details":{"tool_name":"Bash"},"external_metadata":{"new":true}}`
			assertError(t, doCodeSessionWorkerRequestWithMethod(t, app, http.MethodPut, codeSessionID, "", body), http.StatusInternalServerError, "api_error")
			after, err := getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed status write changed private worker state: %v", err)
			}
			if got := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey); !reflect.DeepEqual(history.Data, got.Data) {
				t.Fatal("failed status write changed history")
			}
			afterWatermark, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !afterWatermark.Equal(watermark) {
				t.Fatalf("failed status write advanced event clock: %v", err)
			}
			primary, found, err := app.db.GetPrimarySessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID)
			if err != nil || !found || primary.Status != "running" || mustSessionRecord(t, app, session.ExternalID).Status != "running" {
				t.Fatalf("failed status write changed public state: %v", err)
			}

			removeFailure()
			putCodeSessionWorkerState(t, app, codeSessionID, body)
			after, err = getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || after.WorkerStatus != "requires_action" || mustSessionRecord(t, app, session.ExternalID).Status != "idle" {
				t.Fatalf("worker and public status did not commit together: %v", err)
			}
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(after.WorkerExternalMetadata, &metadata); err != nil || string(metadata["keep"]) != "9007199254740993" || string(metadata["new"]) != "true" {
				t.Fatalf("worker metadata patch lost fields or precision: %v", err)
			}
			var details struct {
				ToolName string `json:"tool_name"`
			}
			if err := json.Unmarshal(after.WorkerRequiresActionDetails, &details); err != nil || details.ToolName != "Bash" {
				t.Fatalf("requires_action details did not commit: %v", err)
			}
			history = listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
			putCodeSessionWorkerState(t, app, codeSessionID, body)
			if got := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey); !reflect.DeepEqual(history.Data, got.Data) {
				t.Fatal("repeated status added another public event")
			}
			putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"worker_status":"running","external_metadata":{"new":null}}`)
			after, err = getCodeSession(app, t.Context(), codeSessionID)
			if err != nil || len(after.WorkerRequiresActionDetails) != 0 || after.WorkerStatus != "running" || mustSessionRecord(t, app, session.ExternalID).Status != "running" {
				t.Fatalf("resume did not clear details and commit both states: %v", err)
			}
			metadata = nil
			if err := json.Unmarshal(after.WorkerExternalMetadata, &metadata); err != nil || len(metadata) != 1 {
				t.Fatalf("metadata null patch did not remove the key: %v", err)
			}
		})
	}
}

func TestWorkerStatusSerializesConcurrentReports(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "worker-status-order", `{"model":"claude-opus-4-6","name":"worker-status-order"}`, `{"name":"worker-status-order"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ExternalID)
	epoch, err := strconv.ParseInt(registerCodeSessionWorker(t, app, codeSessionID), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	putCodeSessionWorkerState(t, app, codeSessionID, `{"worker_epoch":`+strconv.FormatInt(epoch, 10)+`,"worker_status":"idle"}`)
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
	sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, nil)
	results := make(chan error, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			_, err := service.UpdateWorkerState(t.Context(), codeSessionID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: epoch, WorkerStatus: new("running")})
			results <- err
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := listSessionEvents(t, app, session.ExternalID, "types[]=session.status_running&limit=100", defaultTestKey); len(got.Data) != 1 {
		t.Fatalf("concurrent reports created %d running events", len(got.Data))
	}
	if _, err := service.UpdateWorkerState(t.Context(), codeSessionID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: epoch, WorkerStatus: new("idle")}); err != nil {
		t.Fatal(err)
	}
	childID := "sthr_" + uuid.NewV4().String()
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSessionID, epoch, []json.RawMessage{
		json.RawMessage(`{"id":"sevt_create_` + uuid.NewV4().String() + `","type":"session.thread_created","session_thread_id":` + quoteJSON(childID) + `,"agent_name":"child"}`),
		json.RawMessage(`{"id":"sevt_run_` + uuid.NewV4().String() + `","type":"session.thread_status_running","session_thread_id":` + quoteJSON(childID) + `}`),
	}, nil); err != nil {
		t.Fatal(err)
	}
	history := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey)
	if _, err := service.UpdateWorkerState(t.Context(), codeSessionID, db.UpdateCodeSessionWorkerStateInput{WorkerEpoch: epoch, WorkerStatus: new("idle")}); err != nil {
		t.Fatal(err)
	}
	if got := listSessionEvents(t, app, session.ExternalID, "limit=100", defaultTestKey); !reflect.DeepEqual(history.Data, got.Data) {
		t.Fatal("unchanged primary status added an event while a child was running")
	}
	if got := mustSessionRecord(t, app, session.ExternalID).Status; got != "running" {
		t.Fatalf("repeated primary idle overwrote running child aggregate: %s", got)
	}
}
