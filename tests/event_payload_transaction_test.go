package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestEventPayloadSameTransactionRetry(t *testing.T) {
	objects := newFakeStore("payload-transaction-retry")
	app := newPayloadIntegrationApp(t, objects)
	worker, epoch := newPayloadIntegrationSession(t, app)
	store := eventpayload.New(app.db, objects)

	public := publicPayloadEvent("sevt_same_batch", sizedPublicPayload(40000))
	created, err := store.AppendSessionEventsIfAbsent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, []db.SessionEvent{public, public})
	if err != nil || len(created) != 1 {
		t.Fatalf("same-batch public retry: %d events, %v", len(created), err)
	}
	assertRawJSONEqual(t, created[0].Payload, string(public.Payload))

	private := sizedPrivatePayload("same-batch-private", 40000)
	postCodeSessionWorkerInternalEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, private, private))
	page := getCodeSessionWorkerInternalEvents(t, app, worker.ExternalID, "internal-events")
	if len(page.Data) != 1 {
		t.Fatalf("same-batch private retry: %d events", len(page.Data))
	}
	assertRawJSONEqual(t, page.Data[0].Payload, private)
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 2)
}

func TestEventPayloadSubagentMaterializesBeforeCommit(t *testing.T) {
	objects := newFakeStore("payload-transaction-child")
	app := newPayloadIntegrationApp(t, objects)
	worker, epoch := newPayloadIntegrationSession(t, app)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, `{"worker_epoch":`+quoteJSON(epoch)+`,"events":[{"payload":{"type":"system","uuid":"large-child-start","subtype":"task_started","task_id":"large-child","description":"large child"}}]}`)
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
		t.Fatal("missing child thread")
	}
	private := sizedPrivatePayload("large-child-message", 40000)
	body := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"agent_id":"large-child","payload":` + private + `}]}`
	postCodeSessionWorkerInternalEvents(t, app, worker.ExternalID, body)
	// Verify both rows exist before any history read can influence projection.
	assertPayloadSQLCount(t, app, `select count(*) from code_session_internal_events where payload_blob_uuid is not null`, 1)
	assertPayloadSQLCount(t, app, `select count(*) from session_events where thread_external_id=$1 and event_type='agent.message' and payload_blob_uuid is not null`, 1, childID)
	page := listThreadEvents(t, app, worker.SessionExternalID, childID, config.DefaultAPIKey)
	assertPublicPayloadText(t, page.Data, private)
	postCodeSessionWorkerInternalEvents(t, app, worker.ExternalID, body)
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 2)
}

func TestEventPayloadTransactionUsesSingleConnection(t *testing.T) {
	objects := newFakeStore("payload-single-connection")
	app := newPayloadIntegrationApp(t, objects)
	worker, _ := newPayloadIntegrationSession(t, app)
	// Leave only the transaction's connection available. Preparation must release
	// it before registering a blob; retry restoration must use its executor.
	app.db.SQLDB().SetMaxOpenConns(1)
	store := eventpayload.New(app.db, objects)
	event := publicPayloadEvent("sevt_single_connection", sizedPublicPayload(40000))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	created, err := store.AppendSessionEventsIfAbsent(ctx, worker.WorkspaceUUID, worker.SessionExternalID, []db.SessionEvent{event})
	if err != nil || len(created) != 1 {
		t.Fatalf("single-connection write: %d events, %v", len(created), err)
	}
	retried, err := store.AppendSessionEventsIfAbsent(ctx, worker.WorkspaceUUID, worker.SessionExternalID, []db.SessionEvent{event})
	if err != nil || len(retried) != 0 {
		t.Fatalf("single-connection retry: %d events, %v", len(retried), err)
	}
	stored, err := store.GetSessionEvent(ctx, worker.WorkspaceUUID, worker.SessionExternalID, event.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	assertRawJSONEqual(t, stored.Payload, string(event.Payload))
}

func TestLargeToolPermissionPreparesOutsideTransaction(t *testing.T) {
	objects := newFakeStore("payload-large-permission")
	app := newPayloadIntegrationApp(t, objects)
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"large-permission","tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"always_ask"}}}]}`)
	env := createEnvironment(t, app, `{"name":"large-permission"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, codeID)
	body := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"payload":{"type":"control_request","uuid":"large-ask","request_id":"large-ask","request":{"subtype":"can_use_tool","tool_name":"Bash","tool_use_id":"large-tool","input":{"command":` + quoteJSON(strings.Repeat("x", 40000)) + `}}}}]}`
	postCodeSessionWorkerEvents(t, app, codeID, body)
	postCodeSessionWorkerEvents(t, app, codeID, body)
	page := listSessionEvents(t, app, session.ID, "types[]=agent.tool_use", config.DefaultAPIKey)
	if len(page.Data) != 1 {
		t.Fatalf("permission retries created %d events", len(page.Data))
	}
	if !strings.Contains(string(page.Data[0]), strings.Repeat("x", 40000)) {
		t.Fatal("missing full permission input")
	}
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 1)
}
