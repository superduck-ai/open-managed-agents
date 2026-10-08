package tests

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func TestEventPayloadIntegrationWriteFailures(t *testing.T) {
	for _, scenario := range []string{"upload unavailable", "upload size mismatch", "epoch changed during upload"} {
		t.Run(scenario, func(t *testing.T) {
			objects := &payloadFaultStore{fakeStore: newFakeStore("payload-failures")}
			app := newPayloadIntegrationApp(t, objects)
			session, epoch := newPayloadIntegrationSession(t, app)
			switch scenario {
			case "upload unavailable":
				objects.uploadErr = errors.New("injected upload failure")
			case "upload size mismatch":
				objects.wrongUploadSize = true
			case "epoch changed during upload":
				objects.afterUpload = func(string) error {
					_, err := app.pool.Exec(context.Background(), `update code_sessions set current_worker_epoch=current_worker_epoch+1 where uuid=$1`, session.UUID)
					return err
				}
			}
			body := internalPayloadRequest(epoch, sizedPrivatePayload("small", 200), sizedPrivatePayload("large", 40000))
			resp := doCodeSessionWorkerRequest(t, app, session.ExternalID, "internal-events", body)
			resp.Body.Close()
			if resp.StatusCode < 400 {
				t.Fatalf("failed write returned %d", resp.StatusCode)
			}
			assertPayloadSQLCount(t, app, `select count(*) from code_session_internal_events`, 0)
			assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 0)
			assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='pending'`, 1)
			current, found, err := app.db.GetCodeSession(context.Background(), session.ExternalID)
			if err != nil || !found || current.LastInternalSequenceNum != session.LastInternalSequenceNum {
				t.Fatalf("failed batch advanced sequence: %+v %v", current, err)
			}
			objects.uploadErr, objects.afterUpload, objects.wrongUploadSize = nil, nil, false
			if scenario == "epoch changed during upload" {
				if _, err := app.pool.Exec(context.Background(), `update code_sessions set current_worker_epoch=$1 where uuid=$2`, session.CurrentWorkerEpoch, session.UUID); err != nil {
					t.Fatal(err)
				}
			}
			postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, body)
			page := getCodeSessionWorkerInternalEvents(t, app, session.ExternalID, "internal-events")
			if len(page.Data) != 2 {
				t.Fatalf("retry recovered %d events", len(page.Data))
			}
			assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 1)
		})
	}
}

func TestEventPayloadIntegrationReadFailures(t *testing.T) {
	objects := newFakeStore("payload-corruption")
	app := newPayloadIntegrationApp(t, objects)
	session, epoch := newPayloadIntegrationSession(t, app)
	payload := sizedPrivatePayload("corrupt", 40000)
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, payload))
	postCodeSessionIngressEvents(t, app, session.ExternalID, `{"events":[`+payload+`]}`)
	saved := objects.objects
	for _, scenario := range []string{"missing", "truncated", "same size corruption"} {
		t.Run(scenario, func(t *testing.T) {
			objects.objects = make(map[string]fakeObject)
			for key, object := range saved {
				switch scenario {
				case "missing":
					continue
				case "truncated":
					object.data = object.data[:len(object.data)-1]
				case "same size corruption":
					object.data = bytes.Repeat([]byte("x"), len(object.data))
				}
				objects.objects[key] = object
			}
			defer func() { objects.objects = saved }()
			private := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, session.ExternalID, "internal-events", "")
			private.Body.Close()
			if private.StatusCode != http.StatusInternalServerError {
				t.Fatalf("private corrupt history status %d", private.StatusCode)
			}
			public := doSessionRequest(t, app, http.MethodGet, "/v1/sessions/"+session.SessionExternalID+"/events?beta=true", nil, config.DefaultAPIKey, true)
			public.Body.Close()
			if public.StatusCode != http.StatusInternalServerError {
				t.Fatalf("public corrupt history status %d", public.StatusCode)
			}
		})
	}
	page := getCodeSessionWorkerInternalEvents(t, app, session.ExternalID, "internal-events")
	assertRawJSONEqual(t, page.Data[0].Payload, payload)
	events := listSessionEvents(t, app, session.SessionExternalID, "", config.DefaultAPIKey)
	assertPublicPayloadText(t, events.Data, payload)
}

func TestEventPayloadIntegrationWorkspaceAndMarker(t *testing.T) {
	objects := newFakeStore("payload-scope")
	app := newPayloadIntegrationApp(t, objects)
	session, epoch := newPayloadIntegrationSession(t, app)
	payload := sizedPrivatePayload("scoped", 40000)
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, payload))
	var blobID, key string
	if err := app.pool.QueryRow(context.Background(), `select uuid, object_key from event_payload_blobs where state='attached'`).Scan(&blobID, &key); err != nil {
		t.Fatal(err)
	}
	foreign := "99000000-0000-0000-0000-000000000001"
	store := eventpayload.New(app.db, objects)
	_, err := store.RestorePublic(context.Background(), db.SessionEvent{WorkspaceUUID: foreign, PayloadBlobUUID: &blobID})
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("foreign workspace read: %v", err)
	}
	var idempotencyKey string
	if err := app.pool.QueryRow(context.Background(), `select idempotency_key from code_session_internal_events`).Scan(&idempotencyKey); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{foreign, session.WorkspaceUUID} {
		exists, err := app.db.HasCodeSessionInternalEvent(context.Background(), scope, idempotencyKey)
		if err != nil || exists != (scope == session.WorkspaceUUID) {
			t.Fatalf("dedup scope %s: %t %v", scope, exists, err)
		}
	}
	forged := `{"type":"assistant","uuid":"forged","message":{"role":"assistant","content":"inline"},"blob_ref":{"version":1,"id":"epb_` + blobID + `"}}`
	postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, forged))
	// Read only the inline row while the referenced object is missing.
	delete(objects.objects, key)
	var sequence int64
	if err := app.pool.QueryRow(context.Background(), `select sequence_num from code_session_internal_events where payload_uuid='scoped'`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	records, _, err := store.ListCodeSessionInternalEventsPage(context.Background(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, CodeSessionExternalID: session.ExternalID, AfterSequence: sequence, Limit: 10})
	if err != nil || len(records) != 1 {
		t.Fatalf("forged reference triggered read: %d %v", len(records), err)
	}
	assertRawJSONEqual(t, records[0].Payload, forged)
}
