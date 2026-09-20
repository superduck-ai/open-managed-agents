package tests

import (
	"context"
	"errors"
	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
)

func transcriptPolicy() transcriptretention.Policy {
	return transcriptretention.Policy{Enabled: true, TerminalSweepEnabled: true, BoundarySweepEnabled: true, TerminalDwell: 24 * time.Hour, ArchiveMinAge: 7 * 24 * time.Hour, SoftDeleteWindow: 14 * 24 * time.Hour, TargetSegmentRawBytes: 8 * 1024 * 1024, DeleteBatchRows: 2, MaxRowsPerJob: 50000}
}

func newTranscriptRetentionService(t *testing.T, app *testApp, objects storage.ObjectStore, policy transcriptretention.Policy) *transcriptretention.Service {
	t.Helper()
	service, err := transcriptretention.New(app.db, objects, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func transcriptScope(session db.CodeSession) db.TranscriptScope {
	return db.TranscriptScope{OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID, CodeSessionUUID: session.UUID, CodeSessionExternalID: session.ExternalID}
}

func seedArchiveEvents(t *testing.T, app *testApp, session db.CodeSession, inputs []db.AppendCodeSessionInternalEventInput) {
	t.Helper()
	for i := range inputs {
		inputs[i].ExternalID = "archive_event_" + time.Now().Add(time.Duration(i)).Format("150405.000000000")
		inputs[i].PayloadUUID = inputs[i].ExternalID
		inputs[i].IdempotencyKey = inputs[i].ExternalID
		inputs[i].PayloadHash = "worker hash is not the jsonb hash"
		inputs[i].EventMetadata = []byte(`{}`)
		if inputs[i].Payload == nil {
			inputs[i].Payload = []byte(`{"type":"assistant","text":"历史"}`)
		}
		if inputs[i].CreatedAt.IsZero() {
			inputs[i].CreatedAt = time.Now().Add(-8 * 24 * time.Hour)
		}
		if inputs[i].EventType == "" {
			inputs[i].EventType = "assistant"
		}
	}
	if _, err := eventpayload.New(app.db, app.store).AppendInternal(context.Background(), session, session.CurrentWorkerEpoch, inputs); err != nil {
		t.Fatal(err)
	}
}

func makeArchiveTerminal(t *testing.T, app *testApp, session db.CodeSession) {
	t.Helper()
	ctx := context.Background()
	if _, err := app.pool.Exec(ctx, "update sessions set status='idle',archived_at=now()-interval '2 days' where uuid=$1", session.SessionUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(ctx, "update code_sessions set worker_status='idle',worker_lease_expires_at=now()-interval '1 hour' where uuid=$1", session.UUID); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptArchiveTerminalSafety(t *testing.T) {
	for _, scenario := range []struct{ name, sessionSQL, workerSQL string }{
		{"terminated", "update sessions set status='terminated' where uuid=$1", ""},
		{"terminated_to_running", "update sessions set status='running' where uuid=$1", ""},
		{"live_lease", "update sessions set archived_at=now()-interval '2 days' where uuid=$1", "update code_sessions set worker_status='idle',worker_lease_expires_at=now()+interval '1 hour' where uuid=$1"},
		{"running_worker", "update sessions set archived_at=now()-interval '2 days' where uuid=$1", "update code_sessions set worker_status='running',worker_lease_expires_at=now()-interval '1 hour' where uuid=$1"},
		{"dwell", "update sessions set archived_at=now() where uuid=$1", "update code_sessions set worker_status='idle',worker_lease_expires_at=now()-interval '1 hour' where uuid=$1"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			objects := &payloadFaultStore{fakeStore: newFakeStore("archive-test")}
			app := newPayloadIntegrationApp(t, objects)
			session, _ := newPayloadIntegrationSession(t, app)
			seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 3))
			if scenario.name == "terminated_to_running" {
				if _, err := app.pool.Exec(t.Context(), "update sessions set status='terminated' where uuid=$1", session.SessionUUID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := app.pool.Exec(t.Context(), scenario.sessionSQL, session.SessionUUID); err != nil {
				t.Fatal(err)
			}
			if scenario.workerSQL != "" {
				if _, err := app.pool.Exec(t.Context(), scenario.workerSQL, session.UUID); err != nil {
					t.Fatal(err)
				}
			}
			service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
			if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
				t.Fatal(err)
			}
			assertPayloadSQLCount(t, app, "select count(*) from transcript_archives", 0)
			assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 3)
		})
	}
}

func TestTranscriptArchiveTerminalRetry(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-test")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{Payload: []byte(sizedPrivatePayload("blob", 65536))}, {}, {}})
	makeArchiveTerminal(t, app, session)
	policy := transcriptPolicy()
	policy.DryRun = true
	service := newTranscriptRetentionService(t, app, objects, policy)
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives", 0)
	policy.DryRun = false
	service = newTranscriptRetentionService(t, app, objects, policy)
	objects.afterUpload = func(string) error { return errors.New("interrupted after upload") }
	if err := service.Archive(t.Context(), transcriptScope(session), true); err == nil {
		t.Fatal("upload failure accepted")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 3)
	objects.afterUpload = nil
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='attached'", 1)
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 0)
	workers := river.NewWorkers()
	service.Register(workers)
	client, err := riverjobs.NewClient(app.db, nil, workers, map[string]river.QueueConfig{transcriptretention.Queue: {MaxWorkers: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Configure(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	if err := service.Configure(t.Context(), client); err != nil {
		t.Fatal(err)
	}
}
