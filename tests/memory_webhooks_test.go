package tests

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

var memoryWebhookEvents = []string{"memory_store.created", "memory_store.archived", "memory_store.deleted"}

func TestWebhookMemoryRejectedOperations(t *testing.T) {
	app, _, _ := newEventSubscription(t, memoryWebhookEvents)
	store := createWebhookMemoryStore(t, app, "memory notifications")
	const otherKey = "sk-ant-test-memory-webhook-foreign"
	seedWorkspaceKey(t, app.pool, "memory_webhook_org", "memory_webhook_workspace", "memory_webhook_key", otherKey)
	base := "/v1/memory_stores/" + store.ID
	for _, tc := range []struct {
		name, method, path, body, key string
		status                        int
	}{
		{"invalid create", "POST", "/v1/memory_stores", `{"name":""}`, defaultTestKey, 400},
		{"invalid update", "POST", base, `{"name":123}`, defaultTestKey, 400},
		{"missing archive", "POST", "/v1/memory_stores/memstore_missing/archive", `{}`, defaultTestKey, 404},
		{"missing delete", "DELETE", "/v1/memory_stores/memstore_missing", `{}`, defaultTestKey, 404},
		{"foreign archive", "POST", base + "/archive", `{}`, otherKey, 404},
		{"foreign delete", "DELETE", base, `{}`, otherKey, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doMemoryRequest(t, app, tc.method, tc.path+"?beta=true", strings.NewReader(tc.body), tc.key, true)
			errorType := "not_found_error"
			if tc.status == 400 {
				errorType = "invalid_request_error"
			}
			assertError(t, response, tc.status, errorType)
			assertMemoryWebhookCounts(t, app, store.ID, 1, 0, 0)
			assertMemoryWebhookTotal(t, app, 1)
		})
	}
}

func TestWebhookMemoryDatabaseFailures(t *testing.T) {
	app, _, _ := newEventSubscription(t, memoryWebhookEvents)
	store := createWebhookMemoryStore(t, app, "memory fault")
	before := retrieveMemoryStore(t, app, store.ID, defaultTestKey)
	removeFailure := installWebhookMutationFailure(t, app, "memory_stores", "INSERT OR UPDATE", "NEW.name = 'memory fault'")
	for _, tc := range []struct{ path, body string }{
		{"/v1/memory_stores", `{"name":"memory fault"}`},
		{"/v1/memory_stores/" + store.ID + "/archive", `{}`},
	} {
		response := doMemoryRequest(t, app, "POST", tc.path+"?beta=true", strings.NewReader(tc.body), defaultTestKey, true)
		assertError(t, response, 500, "api_error")
	}
	after := retrieveMemoryStore(t, app, store.ID, defaultTestKey)
	if before.UpdatedAt != after.UpdatedAt || after.ArchivedAt != nil {
		t.Fatal("failed archive changed store")
	}
	assertMemoryWebhookCounts(t, app, store.ID, 1, 0, 0)
	assertMemoryWebhookTotal(t, app, 1)
	removeFailure()
}

func TestWebhookMemoryDeleteRollback(t *testing.T) {
	app, _, _ := newEventSubscription(t, memoryWebhookEvents)
	store := createWebhookMemoryStore(t, app, "memory rollback")
	memory := createMemory(t, app, store.ID, "/first.md", "first content")
	updateMemory(t, app, store.ID, memory.ID, `{"content":"second content"}`)
	createMemory(t, app, store.ID, "/second.md", "another content")
	before := memoryWebhookRowCounts(t, app, store.ID)
	objects := len(app.store.(*fakeStore).objects)
	record, err := app.db.GetMemoryStore(t.Context(), getDefaultDBIDs(t, app.pool).WorkspaceUUID, store.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Versions are deleted before this AFTER DELETE trigger on memories rejects the transaction.
	removeFailure := installWebhookMutationFailure(t, app, "memories", "DELETE", "OLD.memory_store_uuid = '"+record.UUID+"'")
	response := doMemoryRequest(t, app, "DELETE", "/v1/memory_stores/"+store.ID+"?beta=true", nil, defaultTestKey, true)
	assertError(t, response, 500, "api_error")
	if after := memoryWebhookRowCounts(t, app, store.ID); after != before {
		t.Fatalf("rollback rows=%v want %v", after, before)
	}
	if len(app.store.(*fakeStore).objects) != objects {
		t.Fatal("rollback removed stored content")
	}
	assertMemoryWebhookCounts(t, app, store.ID, 1, 0, 0)
	removeFailure()
	deleteMemoryStore(t, app, store.ID)
	if after := memoryWebhookRowCounts(t, app, store.ID); after != [3]int{} {
		t.Fatalf("cascade left rows: %v", after)
	}
	assertMemoryWebhookCounts(t, app, store.ID, 1, 0, 1)
}

func TestWebhookMemoryConcurrentOperations(t *testing.T) {
	app, _, _ := newEventSubscription(t, memoryWebhookEvents)
	store := createWebhookMemoryStore(t, app, "concurrent memory")
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() { archiveMemoryStore(t, app, store.ID) })
	}
	group.Wait()
	before := retrieveMemoryStore(t, app, store.ID, defaultTestKey)
	after := archiveMemoryStore(t, app, store.ID)
	if before.UpdatedAt != after.UpdatedAt || before.ArchivedAt == nil || after.ArchivedAt == nil || *before.ArchivedAt != *after.ArchivedAt {
		t.Fatal("duplicate archive changed timestamps")
	}
	assertMemoryWebhookCounts(t, app, store.ID, 1, 1, 0)
	var deleted, missing atomic.Int32
	for range 8 {
		group.Go(func() {
			response := doMemoryRequest(t, app, "DELETE", "/v1/memory_stores/"+store.ID+"?beta=true", nil, defaultTestKey, true)
			defer response.Body.Close()
			switch response.StatusCode {
			case 200:
				deleted.Add(1)
			case 404:
				missing.Add(1)
			default:
				t.Errorf("delete status=%d", response.StatusCode)
			}
		})
	}
	group.Wait()
	if deleted.Load() != 1 || missing.Load() != 7 {
		t.Fatalf("delete success=%d missing=%d", deleted.Load(), missing.Load())
	}
	response := doMemoryRequest(t, app, "DELETE", "/v1/memory_stores/"+store.ID+"?beta=true", nil, defaultTestKey, true)
	assertError(t, response, 404, "not_found_error")
	assertMemoryWebhookCounts(t, app, store.ID, 1, 1, 1)
}

func TestWebhookMemoryOperationsDoNotNotify(t *testing.T) {
	app, _, _ := newEventSubscription(t, memoryWebhookEvents)
	store := createWebhookMemoryStore(t, app, "memory resource boundary")
	updateMemoryStore(t, app, store.ID, `{"description":"changed","metadata":{"extra":"value"}}`)
	memory := createMemory(t, app, store.ID, "/first.md", "first content")
	updated := updateMemory(t, app, store.ID, memory.ID, `{"content":"second content"}`)
	deleteMemory(t, app, store.ID, memory.ID, updated.ContentSHA256)
	assertMemoryWebhookCounts(t, app, store.ID, 1, 0, 0)
	assertMemoryWebhookTotal(t, app, 1)
	deleteMemoryStore(t, app, store.ID)
	assertMemoryWebhookCounts(t, app, store.ID, 1, 0, 1)
}

func TestWebhookMemorySubscriptionFiltering(t *testing.T) {
	app, endpoint, _ := newEventSubscription(t, []string{"memory_store.archived"})
	store := createWebhookMemoryStore(t, app, "filtered memory")
	updateWebhook(t, app, endpoint.ID, `{"status":"disabled"}`)
	archiveMemoryStore(t, app, store.ID)
	updateWebhook(t, app, endpoint.ID, `{"status":"enabled","enabled_events":["memory_store.created","memory_store.archived","memory_store.deleted"]}`)
	archiveMemoryStore(t, app, store.ID)
	assertMemoryWebhookCounts(t, app, store.ID, 0, 0, 0)
	const otherKey = "sk-ant-test-memory-webhook-other"
	seedWorkspaceKey(t, app.pool, "memory_webhook_other_org", "memory_webhook_other_workspace", "memory_webhook_other_key", otherKey)
	response := doMemoryRequest(t, app, "POST", "/v1/memory_stores?beta=true", strings.NewReader(`{"name":"foreign memory"}`), otherKey, true)
	if response.StatusCode != 200 {
		t.Fatalf("foreign create failed: %s", readAll(t, response.Body))
	}
	var other memoryStoreAPIResponse
	decodeJSON(t, response.Body, &other)
	response.Body.Close()
	for _, tc := range []struct{ method, path string }{{"POST", "/archive"}, {"DELETE", ""}} {
		response = doMemoryRequest(t, app, tc.method, "/v1/memory_stores/"+other.ID+tc.path+"?beta=true", nil, otherKey, true)
		if response.StatusCode != 200 {
			t.Errorf("foreign operation=%d", response.StatusCode)
		}
		response.Body.Close()
	}
	assertMemoryWebhookTotal(t, app, 0)
}

type memoryWebhookCleanupProbe struct {
	storage.ObjectStore
	beforeDelete func()
}

func (s *memoryWebhookCleanupProbe) Delete(ctx context.Context, key string, options storage.DeleteOptions) error {
	s.beforeDelete()
	return s.ObjectStore.Delete(ctx, key, options)
}

func TestWebhookMemoryCleanupFailureAndDelivery(t *testing.T) {
	objects := newFakeStore("memory-webhook-cleanup")
	probe := &memoryWebhookCleanupProbe{ObjectStore: objects, beforeDelete: func() {}}
	app, endpoint, received := newEventSubscriptionWithStore(t, memoryWebhookEvents, probe)
	store := createWebhookMemoryStore(t, app, "cleanup memory")
	memory := createMemory(t, app, store.ID, "/first.md", "first content")
	updateMemory(t, app, store.ID, memory.ID, `{"content":"second content"}`)
	createMemory(t, app, store.ID, "/second.md", "another content")
	archiveMemoryStore(t, app, store.ID)
	calls := 0
	probe.beforeDelete = func() {
		calls++
		var count int
		err := app.pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE type='webhook_delivery' AND payload->'event'->'data'->>'id'=$1 AND payload->'event'->'data'->>'type'='memory_store.deleted'`, store.ID).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("deleted event not enqueued before cleanup: count=%d error=%v", count, err)
		}
	}
	objects.deleteErr = errors.New("test storage unavailable")
	deleteMemoryStore(t, app, store.ID)
	if calls != 3 || len(objects.objects) != 3 {
		t.Fatalf("cleanup calls=%d objects=%d want 3", calls, len(objects.objects))
	}
	if rows := memoryWebhookRowCounts(t, app, store.ID); rows != [3]int{} {
		t.Fatalf("deleted rows remain: %v", rows)
	}
	scope := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	t.Cleanup(func() {
		_, err := app.pool.Exec(context.Background(), `DELETE FROM jobs WHERE type='object_cleanup' AND workspace_uuid=$1 AND payload->>'bucket'=$2`, scope, objects.Name())
		if err != nil {
			t.Error(err)
		}
	})
	worker := cleanup.NewWorker(app.db, newFakeStorageClient(objects), time.Second, nil)
	if err := worker.RunOnce(t.Context(), "memory-cleanup-failed"); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE type='object_cleanup' AND workspace_uuid=$1 AND payload->>'bucket'=$2 AND attempts=1`, scope, objects.Name()).Scan(&pending); err != nil || pending != 3 {
		t.Fatalf("retried cleanup jobs=%d: %v", pending, err)
	}
	objects.deleteErr = nil
	if _, err := app.pool.Exec(t.Context(), `UPDATE jobs SET run_after=NOW() WHERE type='object_cleanup' AND workspace_uuid=$1 AND payload->>'bucket'=$2`, scope, objects.Name()); err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(t.Context(), "memory-cleanup-success"); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 0 {
		t.Fatal("cleanup retry did not remove objects")
	}
	assertMemoryWebhookCounts(t, app, store.ID, 1, 1, 1)
	// Inspect the actual wire payload, then pass the same deliveries to the shared SDK verifier.
	expected := map[string]int{}
	for _, event := range memoryWebhookEvents {
		expected[event+"/"+store.ID] = 1
	}
	assertWebhookDeliveries(t, app, endpoint, received, expected, assertResourceWebhookPayload)
}

func createWebhookMemoryStore(t *testing.T, app *testApp, name string) memoryStoreAPIResponse {
	t.Helper()
	store := createMemoryStore(t, app, name)
	t.Cleanup(func() { deleteMemoryStore(t, app, store.ID) })
	return store
}
func assertMemoryWebhookCounts(t *testing.T, app *testApp, id string, created, archived, deleted int) {
	t.Helper()
	for i, want := range []int{created, archived, deleted} {
		assertWebhookCount(t, app, memoryWebhookEvents[i], id, want)
	}
}
func assertMemoryWebhookTotal(t *testing.T, app *testApp, want int) {
	t.Helper()
	var total int
	if err := app.pool.QueryRow(t.Context(), `SELECT count(*) FROM jobs WHERE type='webhook_delivery'`).Scan(&total); err != nil || total != want {
		t.Fatalf("webhook jobs=%d want %d: %v", total, want, err)
	}
}
func memoryWebhookRowCounts(t *testing.T, app *testApp, id string) [3]int {
	t.Helper()
	var counts [3]int
	err := app.pool.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM memory_stores WHERE external_id=$1),
 (SELECT count(*) FROM memories WHERE memory_store_external_id=$1),
 (SELECT count(*) FROM memory_versions WHERE memory_store_external_id=$1)`, id).Scan(&counts[0], &counts[1], &counts[2])
	if err != nil {
		t.Fatal(err)
	}
	return counts
}
