package tests

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestTranscriptArchiveCorruptReadback(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-corrupt")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, make([]db.AppendCodeSessionInternalEventInput, 3))
	makeArchiveTerminal(t, app, session)
	objects.afterUpload = func(key string) error {
		objects.mu.Lock()
		defer objects.mu.Unlock()
		object := objects.objects[key]
		object.data[len(object.data)-1] ^= 1
		objects.objects[key] = object
		return nil
	}
	service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
	if err := service.Archive(t.Context(), transcriptScope(session), true); err == nil {
		t.Fatal("corrupt object accepted")
	}
	assertPayloadSQLCount(t, app, "select count(*) from code_session_internal_events where deleted_at is null", 3)
	assertPayloadSQLCount(t, app, "select count(*) from transcript_archives where state='attached'", 0)
}

func TestTranscriptArchiveSegmentsUseRawBytes(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-segments")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{}, {Payload: []byte(sizedPrivatePayload("oversized", 65536))}, {}})
	makeArchiveTerminal(t, app, session)
	policy := transcriptPolicy()
	policy.TargetSegmentRawBytes = 1024
	service := newTranscriptRetentionService(t, app, objects, policy)
	if err := service.Archive(t.Context(), transcriptScope(session), true); err != nil {
		t.Fatal(err)
	}
	archives, err := app.db.ListTranscriptArchives(t.Context(), transcriptScope(session), 0, 10, true)
	if err != nil || len(archives) != 3 {
		t.Fatalf("segments: %d %v", len(archives), err)
	}
	if archives[1].EventCount != 1 || archives[1].RawBytes <= 1024 {
		t.Fatal("oversized event did not get its own segment")
	}
}
