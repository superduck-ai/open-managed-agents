//go:build integration

package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestBrainImageAttachmentsPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.Up(ctx); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	seedClaimCodeSession(t, ctx, database, 1, "claim", "running")
	input := BrainImageAttachment{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: "53000000-0000-0000-0001-000000000001", SHA256: "digest", MediaType: "image/png", SizeBytes: 42, Bucket: "images", ObjectKey: "images/session/digest"}
	t.Run("upload does not hold a transaction connection", func(t *testing.T) {
		maxConnections := database.Stats().MaxOpenConnections
		database.SetMaxOpenConns(1)
		defer database.SetMaxOpenConns(maxConnections)
		failure := errors.New("deliberate upload failure")
		_, err := store.PutBrainImageAttachment(ctx, input, func() error {
			probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
			defer cancel()
			connection, err := database.Conn(probe)
			if err != nil {
				return err
			}
			if err := connection.Close(); err != nil {
				return err
			}
			return failure
		})
		if !errors.Is(err, failure) {
			t.Fatalf("upload held the only database connection: %v", err)
		}
	})

	uploads := 0
	t.Run("failed upload stays pending and is not readable", func(t *testing.T) {
		failure := errors.New("upload interrupted")
		if _, err := store.PutBrainImageAttachment(ctx, input, func() error { return failure }); !errors.Is(err, failure) {
			t.Fatalf("put = %v", err)
		}
		if _, err := store.GetBrainImageAttachment(ctx, input.WorkspaceUUID, input.SessionUUID, input.SHA256); !errors.Is(err, ErrNotFound) {
			t.Fatalf("pending read = %v", err)
		}
	})
	t.Run("retry and duplicate put publish once", func(t *testing.T) {
		upload := func() error { uploads++; return nil }
		created, err := store.PutBrainImageAttachment(ctx, input, upload)
		if err != nil || !created {
			t.Fatalf("retry = %v, %v", created, err)
		}
		created, err = store.PutBrainImageAttachment(ctx, input, upload)
		if err != nil || created || uploads != 1 {
			t.Fatalf("repeat = %v, %v, uploads=%d", created, err, uploads)
		}
		image, err := store.GetBrainImageAttachment(ctx, input.WorkspaceUUID, input.SessionUUID, input.SHA256)
		if err != nil || image.SizeBytes != 42 || image.MediaType != "image/png" {
			t.Fatalf("read = %+v, %v", image, err)
		}
		for _, scope := range [][2]string{{input.WorkspaceUUID, "53000000-0000-0000-0001-000000000002"}, {"53000000-0000-0000-0000-000000000099", input.SessionUUID}} {
			if _, err := store.GetBrainImageAttachment(ctx, scope[0], scope[1], input.SHA256); !errors.Is(err, ErrNotFound) {
				t.Fatalf("scope = %v", err)
			}
		}
	})
	t.Run("deletion schedules cleanup and rejects new writes", func(t *testing.T) {
		execClaimTest(t, ctx, database, `UPDATE sessions SET deleted_at = now() WHERE uuid = $1`, input.SessionUUID)
		if err := store.ScheduleBrainImageAttachmentCleanup(ctx, 10); err != nil {
			t.Fatal(err)
		}
		if _, err := store.PutBrainImageAttachment(ctx, input, func() error { t.Fatal("deleting image uploaded"); return nil }); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleting put = %v", err)
		}
		jobs, err := store.LeaseObjectCleanupJobs(ctx, "images", 10)
		if err != nil || len(jobs) != 1 || jobs[0].ResourceType != "brain_image_attachment" {
			t.Fatalf("jobs = %+v, %v", jobs, err)
		}
		if err := store.FailObjectCleanupJob(ctx, jobs[0].UUID, 0, "temporary", 0, 10); err != nil {
			t.Fatal(err)
		}
		jobs, err = store.LeaseObjectCleanupJobs(ctx, "images", 10)
		if err != nil || len(jobs) != 1 || jobs[0].Attempts != 1 {
			t.Fatalf("retry jobs = %+v, %v", jobs, err)
		}
		if err := store.CompleteObjectCleanupJob(ctx, jobs[0].UUID, jobs[0].ResourceType); err != nil {
			t.Fatal(err)
		}
		_, found, err := NewBrainImageAttachmentMapper(store.mapperDB).Find(ctx, input.WorkspaceUUID, input.SessionUUID, input.SHA256, false)
		if err != nil || found {
			t.Fatalf("completed row found=%v, %v", found, err)
		}
		if _, err := store.PutBrainImageAttachment(ctx, input, func() error { t.Fatal("deleted session uploaded"); return nil }); !errors.Is(err, ErrNotFound) {
			t.Fatalf("deleted put = %v", err)
		}
	})
	t.Run("expired pending objects are reclaimed", func(t *testing.T) {
		seedClaimCodeSession(t, ctx, database, 2, "claim", "running")
		input.SessionUUID = "53000000-0000-0000-0001-000000000002"
		input.ObjectKey = "images/second/digest"
		_, _ = store.PutBrainImageAttachment(ctx, input, func() error { return errors.New("interrupted") })
		execClaimTest(t, ctx, database, `UPDATE brain_image_attachments SET created_at = now() - interval '25 hours' WHERE session_uuid = $1`, input.SessionUUID)
		if err := store.ScheduleBrainImageAttachmentCleanup(ctx, 10); err != nil {
			t.Fatal(err)
		}
		jobs, err := store.LeaseObjectCleanupJobs(ctx, "images", 10)
		if err != nil || len(jobs) != 1 || jobs[0].Key != input.ObjectKey {
			t.Fatalf("expired jobs = %+v, %v", jobs, err)
		}
	})
}
