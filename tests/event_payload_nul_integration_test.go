//go:build integration

package tests

import (
	"context"
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestEventPayloadSmallNULRoundTripAndReplay(t *testing.T) {
	objects := newFakeStore("payload-small-nul")
	app := newPayloadIntegrationApp(t, objects)
	session, epoch := newPayloadIntegrationSession(t, app)
	private := `{"type":"assistant","uuid":"small-nul","message":{"role":"assistant","content":` + quoteJSON("before\x00after") + `}}`
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, private))
	page := getCodeSessionWorkerInternalEvents(t, app, session.ExternalID, "internal-events")
	if len(page.Data) != 1 {
		t.Fatalf("private count: %d", len(page.Data))
	}
	assertRawJSONEqual(t, page.Data[0].Payload, private)
	objects.uploadErr = errors.New("object storage unavailable during replay")
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, private))
	objects.uploadErr = nil
	if len(objects.objects) != 1 {
		t.Fatalf("private object count: %d", len(objects.objects))
	}
	store := eventpayload.New(app.db, objects)
	payload := `{"type":"agent.message","content":[{"type":"text","text":` + quoteJSON("before\x00after") + `}]}`
	event := publicPayloadEvent("sevt_small_nul", payload)
	created, err := store.AppendSessionEventsIfAbsent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{event})
	if err != nil || len(created.Events) != 1 {
		t.Fatalf("public append: %v", err)
	}
	restored, err := store.GetSessionEvent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, event.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	assertRawJSONEqual(t, restored.Payload, payload)
	objects.uploadErr = errors.New("object storage unavailable during replay")
	replay, err := store.AppendSessionEventsIfAbsent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{event})
	objects.uploadErr = nil
	if err != nil || len(replay.Events) != 0 {
		t.Fatalf("public replay: %v", err)
	}
	if len(objects.objects) != 2 {
		t.Fatalf("NUL object count: %d", len(objects.objects))
	}
	literal := `{"type":"agent.message","content":[{"type":"text","text":` + quoteJSON(`before\u0000after`) + `}]}`
	inline := publicPayloadEvent("sevt_literal_nul_escape", literal)
	_, err = store.AppendSessionEventsIfAbsent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{inline})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.db.GetSessionEvent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, inline.ExternalID)
	if err != nil || saved.PayloadBlobUUID != nil {
		t.Fatalf("literal escape should stay inline: %v", err)
	}
	if len(objects.objects) != 2 {
		t.Fatal("literal escape uploaded an object")
	}
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where size_bytes > 0 and size_bytes <= 32768 and state='attached'`, 2)
}
