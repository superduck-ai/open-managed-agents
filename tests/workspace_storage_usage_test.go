package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/google/uuid"
)

func TestWorkspaceStorageUsageLedger(t *testing.T) {
	t.Run("failure concurrent writers cannot exceed the shared limit", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		const (
			fileSize = int64(6)
			limit    = int64(10)
		)

		start := make(chan struct{})
		errs := make(chan error, 2)
		file := workspaceStorageFile(fixture.workspaceID, fileSize)
		go func() {
			<-start
			errs <- fixture.app.db.CreateFileIfWithinLimit(context.Background(), file, limit)
		}()
		go func() {
			<-start
			_, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
				WorkspaceID:                fixture.workspaceID,
				FilesystemID:               fixture.filesystem.ID,
				Path:                       "/concurrent.txt",
				Blob:                       workspaceStorageBlob(fileSize, nil),
				WorkspaceStorageLimitBytes: limit,
			})
			errs <- err
		}()
		close(start)

		var succeeded, rejected int
		for range 2 {
			err := <-errs
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, db.ErrStorageLimitExceeded):
				rejected++
			default:
				t.Fatalf("concurrent create error = %v", err)
			}
		}
		if succeeded != 1 || rejected != 1 {
			t.Fatalf("concurrent results = %d succeeded, %d rejected; want 1 and 1", succeeded, rejected)
		}
		assertWorkspaceStorageBytes(t, fixture, fileSize)
	})

	t.Run("failure resource write rollback also rolls back reserved bytes", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		file := workspaceStorageFile(fixture.workspaceID, 3)
		if err := fixture.app.db.CreateFile(context.Background(), file); err != nil {
			t.Fatalf("create initial file: %v", err)
		}

		duplicate := workspaceStorageFile(fixture.workspaceID, 7)
		duplicate.ExternalID = file.ExternalID
		if err := fixture.app.db.CreateFileIfWithinLimit(context.Background(), duplicate, 100); err == nil {
			t.Fatal("duplicate file create error = nil")
		}
		assertWorkspaceStorageBytes(t, fixture, 3)
	})

	t.Run("success files and filestore maintain one transactional total", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		file := workspaceStorageFile(fixture.workspaceID, 6)
		if err := fixture.app.db.CreateFileIfWithinLimit(context.Background(), file, 10); err != nil {
			t.Fatalf("create Files API file: %v", err)
		}
		if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:                fixture.workspaceID,
			FilesystemID:               fixture.filesystem.ID,
			Path:                       "/shared.txt",
			Blob:                       workspaceStorageBlob(4, nil),
			WorkspaceStorageLimitBytes: 10,
		}); err != nil {
			t.Fatalf("put Filestore file: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 10)

		if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:                fixture.workspaceID,
			FilesystemID:               fixture.filesystem.ID,
			Path:                       "/shared.txt",
			Blob:                       workspaceStorageBlob(2, nil),
			OverwriteExisting:          true,
			WorkspaceStorageLimitBytes: 10,
		}); err != nil {
			t.Fatalf("overwrite Filestore file: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 8)

		if _, err := fixture.app.db.RemoveFilestoreFile(context.Background(), db.RemoveFilestoreEntryInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/shared.txt",
		}); err != nil {
			t.Fatalf("remove Filestore file: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 6)
		if err := fixture.app.db.SoftDeleteFile(context.Background(), fixture.workspaceID, file.ExternalID); err != nil {
			t.Fatalf("delete Files API file: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 0)
	})

	t.Run("success overwrite move and recursive delete release replaced bytes", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		for _, input := range []struct {
			path string
			size int64
		}{
			{path: "/source.txt", size: 3},
			{path: "/destination.txt", size: 5},
		} {
			if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
				WorkspaceID:  fixture.workspaceID,
				FilesystemID: fixture.filesystem.ID,
				Path:         input.path,
				Blob:         workspaceStorageBlob(input.size, nil),
			}); err != nil {
				t.Fatalf("put %s: %v", input.path, err)
			}
		}
		if _, err := fixture.app.db.MoveFilestoreFile(context.Background(), db.MoveFilestoreFileInput{
			WorkspaceID:       fixture.workspaceID,
			FilesystemID:      fixture.filesystem.ID,
			SourcePath:        "/source.txt",
			DestinationPath:   "/destination.txt",
			OverwriteExisting: true,
		}); err != nil {
			t.Fatalf("move over destination: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 3)

		if _, err := fixture.app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/tree",
		}); err != nil {
			t.Fatalf("make directory: %v", err)
		}
		for _, path := range []string{"/tree/a.txt", "/tree/b.txt"} {
			if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
				WorkspaceID:  fixture.workspaceID,
				FilesystemID: fixture.filesystem.ID,
				Path:         path,
				Blob:         workspaceStorageBlob(2, nil),
			}); err != nil {
				t.Fatalf("put %s: %v", path, err)
			}
		}
		assertWorkspaceStorageBytes(t, fixture, 7)
		if _, err := fixture.app.db.RemoveFilestoreDirectory(context.Background(), db.RemoveFilestoreDirectoryInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/tree",
			Recursive:    true,
		}); err != nil {
			t.Fatalf("remove directory recursively: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 3)
	})

	t.Run("success expired bytes are released by the ttl transaction", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		expiresAt := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
		if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/expired.txt",
			Blob:         workspaceStorageBlob(4, &expiresAt),
		}); err != nil {
			t.Fatalf("put expired file: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 4)
		if _, err := fixture.app.db.GetFilestoreEntry(context.Background(), fixture.workspaceID, fixture.filesystem.ID, "/expired.txt"); !errors.Is(err, db.ErrNotFound) {
			t.Fatalf("read expired file error = %v, want ErrNotFound", err)
		}
		if _, err := fixture.app.db.ExpireFilestoreEntries(context.Background(), 1000); err != nil {
			t.Fatalf("expire Filestore entries: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 0)
	})

	t.Run("success namespace reuse releases expired destination bytes", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		expiresAt := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
		if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/directory",
			Blob:         workspaceStorageBlob(4, &expiresAt),
		}); err != nil {
			t.Fatalf("put expired directory destination: %v", err)
		}
		if _, err := fixture.app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/directory",
		}); err != nil {
			t.Fatalf("replace expired file with directory: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 0)

		if _, err := fixture.app.db.MakeFilestoreDirectory(context.Background(), db.MakeFilestoreDirectoryInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/source",
		}); err != nil {
			t.Fatalf("make source directory: %v", err)
		}
		if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/destination",
			Blob:         workspaceStorageBlob(5, &expiresAt),
		}); err != nil {
			t.Fatalf("put expired move destination: %v", err)
		}
		if _, err := fixture.app.db.MoveFilestoreDirectory(context.Background(), db.MoveFilestoreDirectoryInput{
			WorkspaceID:     fixture.workspaceID,
			FilesystemID:    fixture.filesystem.ID,
			SourcePath:      "/source",
			DestinationPath: "/destination",
		}); err != nil {
			t.Fatalf("move directory over expired destination: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 0)
	})

	t.Run("success reconciliation repairs an out of band drift", func(t *testing.T) {
		fixture := newWorkspaceStorageFixture(t)
		file := workspaceStorageFile(fixture.workspaceID, 9)
		if err := fixture.app.db.CreateFile(context.Background(), file); err != nil {
			t.Fatalf("create file: %v", err)
		}
		if _, err := fixture.app.db.PutFilestoreFile(context.Background(), db.PutFilestoreFileInput{
			WorkspaceID:  fixture.workspaceID,
			FilesystemID: fixture.filesystem.ID,
			Path:         "/reconcile.bin",
			Blob:         workspaceStorageBlob(4, nil),
		}); err != nil {
			t.Fatalf("create Filestore file: %v", err)
		}
		if _, err := fixture.app.db.Pool.Exec(context.Background(), `
			update workspace_storage_usage
			set files_bytes = 1, filestore_bytes = 1
			where workspace_id = $1
		`, fixture.workspaceID); err != nil {
			t.Fatalf("inject usage drift: %v", err)
		}
		assertWorkspaceStorageBytes(t, fixture, 2)

		total, err := fixture.app.db.ReconcileWorkspaceStorageUsage(context.Background(), fixture.workspaceID)
		if err != nil {
			t.Fatalf("reconcile workspace storage usage: %v", err)
		}
		if total != 13 {
			t.Fatalf("reconciled total = %d, want 13", total)
		}
		assertWorkspaceStorageBytes(t, fixture, 13)
	})
}

type workspaceStorageFixture struct {
	app         *testApp
	workspaceID int64
	filesystem  db.FilestoreFilesystem
}

func newWorkspaceStorageFixture(t *testing.T) workspaceStorageFixture {
	t.Helper()
	app := newTestAppWithStore(t, nil, newFakeStore("workspace-storage-"+uuid.NewString()))
	t.Cleanup(app.close)
	_, workspaceID, organizationUUID, workspaceUUID, _, _, _, sessionUUID, codeSessionUUID, apiKeyUUID := seedFilestoreLookupScope(t, app)
	filesystem, created, err := app.db.ProvisionFilestoreFilesystem(context.Background(), db.ProvisionFilestoreFilesystemInput{
		UUID:                uuid.NewString(),
		ExternalID:          "fs_storage_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OrganizationUUID:    organizationUUID,
		WorkspaceUUID:       workspaceUUID,
		SessionUUID:         sessionUUID,
		CodeSessionUUID:     stringPointer(codeSessionUUID),
		CreatedByAPIKeyUUID: stringPointer(apiKeyUUID),
		Now:                 time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("provision Filestore filesystem: %v", err)
	}
	if !created {
		t.Fatal("provision Filestore filesystem created = false")
	}
	return workspaceStorageFixture{app: app, workspaceID: workspaceID, filesystem: filesystem}
}

func workspaceStorageFile(workspaceID, sizeBytes int64) db.FileRecord {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	return db.FileRecord{
		UUID:              uuid.NewString(),
		ExternalID:        "file_storage_" + suffix,
		WorkspaceID:       workspaceID,
		Filename:          "storage.txt",
		MimeType:          "text/plain",
		SizeBytes:         sizeBytes,
		SHA256:            strings.Repeat("a", 64),
		S3Bucket:          "workspace-storage",
		S3Key:             "objects/" + suffix,
		Downloadable:      true,
		CreatedByAPIKeyID: 1,
		CreatedAt:         time.Now().UTC(),
	}
}

func workspaceStorageBlob(sizeBytes int64, expiresAt *time.Time) db.FilestoreFileBlob {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	return db.FilestoreFileBlob{
		SizeBytes: sizeBytes,
		MediaType: "text/plain",
		MD5:       "098f6bcd4621d373cade4e832627b4f6",
		SHA256:    strings.Repeat("b", 64),
		S3Bucket:  "workspace-storage",
		S3Key:     "filestore/" + suffix,
		ExpiresAt: expiresAt,
	}
}

func assertWorkspaceStorageBytes(t *testing.T, fixture workspaceStorageFixture, want int64) {
	t.Helper()
	got, err := fixture.app.db.WorkspaceStorageBytes(context.Background(), fixture.workspaceID)
	if err != nil {
		t.Fatalf("read workspace storage bytes: %v", err)
	}
	if got != want {
		t.Fatalf("workspace storage bytes = %d, want %d", got, want)
	}
}
