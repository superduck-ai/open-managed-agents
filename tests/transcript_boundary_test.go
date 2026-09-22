package tests

import (
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
	"reflect"
	"testing"
	"time"
)

func TestTranscriptArchiveBoundaryMinAgePreservesIdempotency(t *testing.T) {
	for _, age := range []time.Duration{0, time.Hour, 7*24*time.Hour - time.Nanosecond, 7 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			objects := &payloadFaultStore{fakeStore: newFakeStore("archive-min-age")}
			app := newPayloadIntegrationApp(t, objects)
			session, _ := newPayloadIntegrationSession(t, app)
			inputs := []db.AppendCodeSessionInternalEventInput{
				{CreatedAt: time.Now().Add(-time.Minute)},
				{CreatedAt: time.Now(), IsCompaction: true},
			}
			seedArchiveEvents(t, app, session, inputs)
			policy := transcriptPolicy()
			policy.ArchiveMinAge = age
			service, err := transcriptretention.New(app.db, objects, policy, nil)
			if age < 7*24*time.Hour {
				if err == nil || service != nil {
					t.Fatalf("unsafe policy accepted: service=%v, error=%v", service, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := service.Archive(t.Context(), transcriptScope(session), false); err != nil {
					t.Fatal(err)
				}
			}
			assertPayloadSQLCount(t, app, "select count(*) from transcript_archives", 0)
			assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is not null", 0)
			retried, err := eventpayload.New(app.db, objects).AppendInternal(t.Context(), session, session.CurrentWorkerEpoch, inputs[:1])
			if err != nil {
				t.Fatal(err)
			}
			if len(retried) != 0 {
				t.Fatalf("same-epoch retry reinserted history: %+v", retried)
			}
			assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events", 2)
			page, _, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, CodeSessionExternalID: session.ExternalID, Limit: 500})
			if err != nil {
				t.Fatal(err)
			}
			if len(page) != 1 || !page[0].IsCompaction {
				t.Fatalf("compacted history became visible: %+v", page)
			}
		})
	}
}

func TestTranscriptArchiveBoundaryAndTerminal(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-test")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	agent := "agent_uncompacted"
	seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{}, {AgentID: &agent}, {IsCompaction: true}, {}})
	query := db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, CodeSessionExternalID: session.ExternalID, Limit: 500}
	before, _, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
	if err := service.Archive(t.Context(), transcriptScope(session), false); err != nil {
		t.Fatal(err)
	}
	after, _, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("resume page changed")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is not null", 1)
	query.Subagents = true
	subagents, _, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), query)
	if err != nil || len(subagents) != 1 {
		t.Fatalf("uncompacted subagent lost: %d %v", len(subagents), err)
	}
	makeArchiveTerminal(t, app, session)
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 0)
	assertPayloadSQLCount(t, app, "select sum(event_count) from transcript_archives where state='attached'", 4)
}

func TestTranscriptArchivePagedSparseHistory(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-pages")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	inputs := make([]db.AppendCodeSessionInternalEventInput, 1105)
	inputs[3].IsCompaction = true
	seedArchiveEvents(t, app, session, inputs)
	if _, err := app.pool.Exec(t.Context(), "update code_session_internal_events set sequence_num=sequence_num+100000"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(t.Context(), "update code_session_internal_events set sequence_num=(sequence_num-100000)*10"); err != nil {
		t.Fatal(err)
	}
	read := func() []db.CodeSessionInternalEvent {
		var all []db.CodeSessionInternalEvent
		query := db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, CodeSessionExternalID: session.ExternalID, Limit: 500}
		for {
			page, more, err := app.db.ListCodeSessionInternalEventsPage(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, page...)
			if !more {
				break
			}
			query.AfterSequence = page[len(page)-1].SequenceNum
		}
		return all
	}
	before := read()
	if len(before) != 1102 {
		t.Fatalf("fixture: %d", len(before))
	}
	service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
	if err := service.Archive(t.Context(), transcriptScope(session), false); err != nil {
		t.Fatal(err)
	}
	after := read()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("paged history changed")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is not null", 3)
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where is_compaction and deleted_at is null", 1)
}
