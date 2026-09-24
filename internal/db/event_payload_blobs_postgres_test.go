package db

import (
	"errors"
	"github.com/superduck-ai/yourbatis"
	"os"
	"testing"
	"time"
)

func TestEventPayloadBlobsPostgres(t *testing.T) {
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, url)
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	mapper := NewEventPayloadBlobMapper(store.mapperDB)
	blob := EventPayloadBlob{UUID: "52000000-0000-0000-0000-000000000090", ExternalID: "epb_test", OrganizationUUID: "52000000-0000-0000-0000-000000000001", WorkspaceUUID: "52000000-0000-0000-0000-000000000002", Bucket: "history", Key: "event/test.json", Size: 32769, SHA256: "digest"}
	if err := store.RegisterEventPayloadBlob(ctx, blob); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetEventPayloadBlob(ctx, blob.WorkspaceUUID, blob.UUID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending visible: %v", err)
	}
	if rows, err := mapper.Attach(ctx, blob.OrganizationUUID, blob.UUID); err != nil || rows != 0 {
		t.Fatalf("foreign scope attach: %d %v", rows, err)
	}
	rollback := errors.New("rollback")
	err := store.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		if err := attachEventPayloadBlob(ctx, tx, blob.WorkspaceUUID, &blob.UUID); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := store.GetEventPayloadBlob(ctx, blob.WorkspaceUUID, blob.UUID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled back attach visible: %v", err)
	}
	now := time.Now()
	err = store.mapperDB.Transaction(ctx, func(tx yourbatis.Executor) error {
		if err := attachEventPayloadBlob(ctx, tx, blob.WorkspaceUUID, &blob.UUID); err != nil {
			return err
		}
		_, err := NewSessionEventMapper(tx).Insert(ctx, sessionEventWriteParams{UUID: "52000000-0000-0000-0000-000000000091", ExternalID: "event_test", OrganizationUUID: blob.OrganizationUUID, WorkspaceUUID: blob.WorkspaceUUID, SessionUUID: "52000000-0000-0000-0000-000000000010", SessionExternalID: "session_test", EventType: "user.message", Payload: []byte(`{"type":"user.message","preview":"hello"}`), PayloadBlobUUID: &blob.UUID, ToolUseID: &blob.ExternalID, CreatedAt: now, ProcessedAt: &now})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetEventPayloadBlob(ctx, blob.WorkspaceUUID, blob.UUID)
	if err != nil || got != blob {
		t.Fatalf("blob roundtrip: %+v %v", got, err)
	}
	event, err := NewSessionEventMapper(store.mapperDB).FindByExternalID(ctx, blob.WorkspaceUUID, "session_test", "event_test")
	if err != nil || event.PayloadBlobUUID == nil || *event.PayloadBlobUUID != blob.UUID || event.ToolUseID == nil || *event.ToolUseID != blob.ExternalID {
		t.Fatalf("event roundtrip: %+v %v", event, err)
	}
	if err := attachEventPayloadBlob(ctx, store.mapperDB, blob.WorkspaceUUID, &blob.UUID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("repeated attachment: %v", err)
	}
	if _, err := database.ExecContext(ctx, "update event_payload_blobs set updated_at=now()-interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	if err := store.ScheduleEventPayloadCleanup(ctx, 10); err != nil {
		t.Fatal(err)
	}
	jobs, err := store.LeaseObjectCleanupJobs(ctx, "test", 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("live event scheduled for deletion: %+v %v", jobs, err)
	}
	if _, err := NewSessionEventMapper(store.mapperDB).SoftDeleteBySession(ctx, blob.WorkspaceUUID, "session_test"); err != nil {
		t.Fatal(err)
	}
	if err := store.ScheduleEventPayloadCleanup(ctx, 10); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.LeaseObjectCleanupJobs(ctx, "test", 10)
	if err != nil || len(jobs) != 1 || jobs[0].ResourceType != "event_payload" || jobs[0].Key != blob.Key {
		t.Fatalf("deleted event cleanup: %+v %v", jobs, err)
	}
	if err := attachEventPayloadBlob(ctx, store.mapperDB, blob.WorkspaceUUID, &blob.UUID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cleanup claimed blob attached: %v", err)
	}
	if err := store.CompleteObjectCleanupJob(ctx, jobs[0].UUID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "update event_payload_blobs set updated_at=now()-interval '2 days'"); err != nil {
		t.Fatal(err)
	}
	if err := store.ScheduleEventPayloadCleanup(ctx, 10); err != nil {
		t.Fatal(err)
	}
	jobs, err = store.LeaseObjectCleanupJobs(ctx, "after-completion", 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("completed cleanup scheduled again: %+v %v", jobs, err)
	}
	var count int
	if err := database.QueryRowContext(ctx, "select count(*) from jobs").Scan(&count); err != nil || count != 1 {
		t.Fatalf("cleanup must create exactly one task: %d %v", count, err)
	}

}
