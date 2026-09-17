package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/eventpayload"
)

func publicPayloadEvent(id, payload string) db.SessionEvent {
	return db.SessionEvent{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "user.message", Payload: json.RawMessage(payload), CreatedAt: time.Now().UTC(), ProcessedAt: time.Now().UTC()}
}

func TestEventPayloadIntegrationBoundaries(t *testing.T) {
	objects := newFakeStore("payload-boundaries")
	app := newPayloadIntegrationApp(t, objects)
	session, epoch := newPayloadIntegrationSession(t, app)
	store := eventpayload.New(app.db, objects)
	var expectedPrivate []string
	largeCount := 0
	for _, alphabet := range []string{"ascii", "utf8"} {
		for _, size := range []int{32767, 32768, 32769} {
			t.Run(fmt.Sprintf("%s/%d", alphabet, size), func(t *testing.T) {
				id := fmt.Sprintf("%s-%d", alphabet, size)
				payload := sizedPrivatePayload(id, size)
				if alphabet == "utf8" {
					// Put a three-byte character across byte 512 while retaining the exact size.
					prefix, suffix := payload[:510], payload[513:]
					payload = prefix + "中" + suffix
				}
				expectedPrivate = append(expectedPrivate, payload)
				postCodeSessionWorkerInternalEvents(t, app, session.ExternalID, internalPayloadRequest(epoch, payload))
				var raw []byte
				var marker *string
				if err := app.pool.QueryRow(context.Background(), `select payload,payload_blob_uuid from code_session_internal_events where payload_uuid=$1`, id).Scan(&raw, &marker); err != nil {
					t.Fatal(err)
				}
				if (marker != nil) != (size > 32768) {
					t.Fatalf("private threshold %d: %v", size, marker)
				}
				if marker == nil {
					assertRawJSONEqual(t, raw, payload)
				} else {
					assertStoredPayloadSummary(t, raw, size)
				}
				publicPayload := sizedPublicPayload(size)
				public := publicPayloadEvent("ev_"+id, publicPayload)
				created, err := store.AppendSessionEvents(context.Background(), session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{public}, nil)
				if err != nil || len(created) != 1 {
					t.Fatalf("public write: %d %v", len(created), err)
				}
				assertRawJSONEqual(t, created[0].Payload, publicPayload)
				stored, err := app.db.GetSessionEvent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, public.ExternalID)
				if err != nil || (stored.PayloadBlobUUID != nil) != (size > 32768) {
					t.Fatalf("public threshold: %+v %v", stored.PayloadBlobUUID, err)
				}
				got, err := store.GetSessionEvent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, public.ExternalID)
				if err != nil {
					t.Fatal(err)
				}
				assertRawJSONEqual(t, got.Payload, publicPayload)
				if size > 32768 {
					largeCount += 2
					var summary eventpayload.Summary
					if err := json.Unmarshal(raw, &summary); err != nil {
						t.Fatal(err)
					}
					if !utf8.ValidString(summary.Preview) || len(summary.Preview) > 512 || !strings.HasPrefix(payload, summary.Preview) {
						t.Fatal("invalid byte preview")
					}
					if alphabet == "utf8" && len(summary.Preview) != 510 {
						t.Fatalf("UTF-8 preview length %d", len(summary.Preview))
					}
					blob, err := app.db.GetEventPayloadBlob(context.Background(), session.WorkspaceUUID, *marker)
					if err != nil {
						t.Fatal(err)
					}
					digest := sha256.Sum256([]byte(payload))
					if blob.Size != int64(size) || blob.SHA256 != hex.EncodeToString(digest[:]) || !bytes.Equal(objects.objects[blob.Key].data, []byte(payload)) {
						t.Fatal("uploaded bytes/registry metadata changed")
					}
				}
				if len(objects.objects) != largeCount {
					t.Fatalf("object count %d want %d", len(objects.objects), largeCount)
				}
			})
		}
	}
	page := getCodeSessionWorkerInternalEvents(t, app, session.ExternalID, "internal-events")
	if len(page.Data) != len(expectedPrivate) {
		t.Fatalf("mixed history length %d", len(page.Data))
	}
	for i, event := range page.Data {
		assertRawJSONEqual(t, event.Payload, expectedPrivate[i])
	}
	// Service pagination must hydrate a page containing both inline and S3 records.
	pageEvents, more, err := store.ListSessionEventsPage(context.Background(), db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.SessionExternalID, Limit: 3, Order: "asc", Types: []string{"user.message"}})
	if err != nil || len(pageEvents) != 3 || !more {
		t.Fatalf("pagination: %d %t %v", len(pageEvents), more, err)
	}
	last := pageEvents[len(pageEvents)-1]
	next, more, err := store.ListSessionEventsPage(context.Background(), db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.SessionExternalID, Limit: 3, Order: "asc", Types: []string{"user.message"}, Cursor: &db.SessionEventPageCursor{CreatedAt: last.CreatedAt, UUID: last.UUID}})
	if err != nil || len(next) != 3 || more {
		t.Fatalf("next page: %d %t %v", len(next), more, err)
	}
	seen := make(map[string]bool)
	for _, event := range append(pageEvents, next...) {
		if seen[event.ExternalID] {
			t.Fatal("pagination repeated an event")
		}
		seen[event.ExternalID] = true
		size := 32767
		if strings.HasSuffix(event.ExternalID, "32768") {
			size = 32768
		}
		if strings.HasSuffix(event.ExternalID, "32769") {
			size = 32769
		}
		assertRawJSONEqual(t, event.Payload, sizedPublicPayload(size))
	}
	// A retry with the same public ID must neither replace history nor upload its new body.
	originalID := "ev_ascii-32769"
	before := len(objects.objects)
	retried, err := store.AppendSessionEventsIfAbsent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{publicPayloadEvent(originalID, sizedPublicPayload(40000))})
	if err != nil || len(retried) != 0 || len(objects.objects) != before {
		t.Fatalf("public replay: %d %v", len(retried), err)
	}
	persisted, err := store.GetSessionEvent(context.Background(), session.WorkspaceUUID, session.SessionExternalID, originalID)
	if err != nil {
		t.Fatal(err)
	}
	assertRawJSONEqual(t, persisted.Payload, sizedPublicPayload(32769))
}

func TestEventPayloadIntegrationConcurrentReplay(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("payload-concurrent")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	store := eventpayload.New(app.db, objects)
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	objects.afterUpload = func(string) error { ready <- struct{}{}; <-release; return nil }
	payload := sizedPrivatePayload("concurrent", 40000)
	input := db.AppendCodeSessionInternalEventInput{ExternalID: "cie_concurrent", EventType: "assistant", PayloadUUID: "concurrent", IdempotencyKey: "concurrent-key", Payload: json.RawMessage(payload), EventMetadata: json.RawMessage(`{}`)}
	type result struct {
		events []db.CodeSessionInternalEvent
		err    error
	}
	results := make(chan result, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			events, err := store.AppendInternal(context.Background(), session, session.CurrentWorkerEpoch, []db.AppendCodeSessionInternalEventInput{input})
			results <- result{events, err}
		}()
	}
	// Always release blocked uploads before cleaning up the test database.
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		workers.Wait()
	}()
	for range 2 {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent requests did not both reach upload")
		}
	}
	close(release)
	workers.Wait()
	inserted := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		inserted += len(result.events)
		for _, event := range result.events {
			assertRawJSONEqual(t, event.Payload, payload)
		}
	}
	if inserted != 1 {
		t.Fatalf("concurrent insert count %d", inserted)
	}
	assertPayloadSQLCount(t, app, `select count(*) from code_session_internal_events`, 1)
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 1)
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='pending'`, 1)
	current, found, err := app.db.GetCodeSession(context.Background(), session.ExternalID)
	if err != nil || !found || current.LastInternalSequenceNum != session.LastInternalSequenceNum+1 {
		t.Fatalf("concurrent sequence: %+v %v", current, err)
	}
}

func sizedPublicPayload(size int) string {
	prefix := `{"type":"user.message","content":[{"type":"text","text":"`
	suffix := `"}]}`
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

func TestEventPayloadIntegrationLimitsAndMissingStore(t *testing.T) {
	objects := newFakeStore("payload-limits")
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	ctx := context.Background()
	noStorage := eventpayload.New(app.db, nil)
	if _, err := noStorage.AppendSessionEvents(ctx, session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{publicPayloadEvent("ev_missing_storage", sizedPublicPayload(32769))}, nil); err == nil {
		t.Fatal("large write without storage succeeded")
	}
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs`, 0)
	store := eventpayload.New(app.db, objects)
	if _, err := store.AppendSessionEvents(ctx, session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{publicPayloadEvent("ev_too_large", sizedPublicPayload(eventpayload.MaxBytes+1))}, nil); err == nil {
		t.Fatal("oversized event accepted")
	}
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs`, 0)
	if len(objects.objects) != 0 {
		t.Fatal("rejected payload uploaded")
	}
	for _, tc := range []struct {
		id    string
		size  int
		store *eventpayload.Store
	}{
		{"ev_inline_without_storage", 32768, noStorage},
		{"ev_maximum", eventpayload.MaxBytes, store},
	} {
		payload := sizedPublicPayload(tc.size)
		created, err := tc.store.AppendSessionEvents(ctx, session.WorkspaceUUID, session.SessionExternalID, []db.SessionEvent{publicPayloadEvent(tc.id, payload)}, nil)
		if err != nil || len(created) != 1 {
			t.Fatalf("accepted boundary %d: %v", tc.size, err)
		}
		restored, err := tc.store.GetSessionEvent(ctx, session.WorkspaceUUID, session.SessionExternalID, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		assertRawJSONEqual(t, restored.Payload, payload)
	}
	assertPayloadSQLCount(t, app, `select count(*) from event_payload_blobs where state='attached'`, 1)
}
