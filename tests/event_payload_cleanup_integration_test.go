package tests

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestEventPayloadIntegrationCleanupLifecycle(t *testing.T) {
	ctx := context.Background()
	objects := &payloadFaultStore{fakeStore: newFakeStore("payload-cleanup")}
	app := newPayloadIntegrationApp(t, objects)
	session, epoch := newPayloadIntegrationSession(t, app)
	store := eventpayload.New(app.db, objects)
	payload := sizedPrivatePayload("live-private", 40000)
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, payload))
	if _, err := store.AppendSessionEvents(ctx, session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{publicPayloadEvent("ev_live", sizedPublicPayload(40000))}, nil); err != nil {
		t.Fatal(err)
	}
	// An upload that reaches storage but fails verification leaves an unreferenced object.
	objects.wrongUploadSize = true
	resp := doCodeSessionWorkerRequest(t, app, session.ExternalID, "internal-events", internalPayloadRequest(epoch, sizedPrivatePayload("orphan", 40000)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("orphan setup status %d", resp.StatusCode)
	}
	objects.wrongUploadSize = false
	worker := cleanup.NewWorker(app.db, newFakeStorageClient(objects), 0, nil)
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 3 || len(objects.deleteOptions) != 0 {
		t.Fatal("recent or live object deleted")
	}
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup'`, 0)
	if _, err := app.pool.Exec(ctx, `update event_payload_blobs set updated_at=now()-interval '2 days'`); err != nil {
		t.Fatal(err)
	}
	// If job creation fails, claiming the blob must roll back in the same transaction.
	if _, err := app.pool.Exec(ctx, `alter table jobs add constraint reject_cleanup_test check (type <> 'object_cleanup')`); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(ctx, "payload-cleanup"); err == nil {
		t.Fatal("job creation failure was ignored")
	}
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='deleting'`, 0)
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup'`, 0)
	if _, err := app.pool.Exec(ctx, `alter table jobs drop constraint reject_cleanup_test`); err != nil {
		t.Fatal(err)
	}
	objects.deleteErr = errors.New("injected delete failure")
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup' and status='retry' and attempts=1`, 1)
	if len(objects.objects) != 3 {
		t.Fatal("failed delete lost object")
	}
	// Re-running the scheduler while the job waits must neither duplicate nor execute it early.
	if _, err := app.pool.Exec(ctx, `update event_payload_blobs set updated_at=now()-interval '2 days' where state='deleting'`); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	if len(objects.deleteOptions) != 1 {
		t.Fatal("retry ran before run_after")
	}
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup'`, 1)
	objects.deleteErr = nil
	if _, err := app.pool.Exec(ctx, `update jobs set run_after=now()-interval '1 second' where type='object_cleanup' and status='retry'`); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 2 {
		t.Fatalf("orphan cleanup left %d objects", len(objects.objects))
	}
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup' and status='completed'`, 1)
	// Both private-only and public-only active references independently protect their blobs.
	private := getCodeSessionWorkerInternalEvents(t, app, session.ExternalID, "internal-events")
	assertRawJSONEqual(t, private.Data[0].Payload, payload)
	if _, err := store.GetSessionEvent(ctx, session.WorkspaceUUID, session.SessionExternalID, "ev_live"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(ctx, `update code_session_internal_events set deleted_at=now() where code_session_uuid=$1`, session.UUID); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 1 {
		t.Fatal("private deletion did not preserve public object")
	}
	if _, err := store.GetSessionEvent(ctx, session.WorkspaceUUID, session.SessionExternalID, "ev_live"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(ctx, `update session_events set deleted_at=now() where external_id='ev_live'`); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 0 {
		t.Fatal("deleted event object retained")
	}
	for _, opts := range objects.deleteOptions {
		if !opts.AllVersions {
			t.Fatal("event cleanup did not request all versions")
		}
	}
	// Move time forward for all tombstones, then prove there is no second cleanup task.
	if _, err := app.pool.Exec(ctx, `update event_payload_blobs set updated_at=now()-interval '30 days'`); err != nil {
		t.Fatal(err)
	}
	deletes := len(objects.deleteOptions)
	if err := worker.RunOnce(ctx, "payload-cleanup"); err != nil {
		t.Fatal(err)
	}
	if len(objects.deleteOptions) != deletes {
		t.Fatal("completed cleanup repeated")
	}
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup'`, 3)
	assertPayloadSQLCount(t, app, `select count(*) from jobs where type='object_cleanup' and status='completed'`, 3)
}
