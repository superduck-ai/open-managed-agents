package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(notFound("missing")) || IsNotFound(invalidArgument("bad")) || IsNotFound(nil) {
		t.Fatal("not found detection mismatched")
	}
}

func TestReadSessionFileMissing(t *testing.T) {
	filesystem := serviceTestFilesystem()
	service := newServiceUnderTest(filestoreTestConfig(0, 0, "filestore-test"), &fakeServiceDatabase{
		getSessionFilesystemFn: func(context.Context, string, string) (db.FilestoreFilesystem, error) {
			return db.FilestoreFilesystem{}, db.ErrNotFound
		},
	}, &fakeServiceBlobStore{})
	if _, err := service.ReadSessionFile(context.Background(), filesystem.WorkspaceUUID, "sesn_missing", "/outputs/score.json", 32); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("missing filesystem error = %v", err)
	}

	service = newServiceUnderTest(filestoreTestConfig(0, 0, "filestore-test"), &fakeServiceDatabase{
		getSessionFilesystemFn: func(context.Context, string, string) (db.FilestoreFilesystem, error) {
			return filesystem, nil
		},
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(context.Context, string, string, string) (db.SessionResourceFile, error) {
			return db.SessionResourceFile{}, db.ErrNotFound
		},
	}, &fakeServiceBlobStore{})
	if _, err := service.ReadSessionFile(context.Background(), filesystem.WorkspaceUUID, "sesn_score", "/outputs/score.json", 32); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("missing file error = %v", err)
	}
}

func TestReadSessionFileReturnsBytes(t *testing.T) {
	filesystem := serviceTestFilesystem()
	payload := []byte(`{"score":1}`)
	entry := serviceTestFileEntry(filesystem, "/outputs/score.json", payload)
	service := newServiceUnderTest(filestoreTestConfig(0, 0, "filestore-test"), &fakeServiceDatabase{
		getSessionFilesystemFn: func(context.Context, string, string) (db.FilestoreFilesystem, error) {
			return filesystem, nil
		},
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(context.Context, string, string, string) (db.SessionResourceFile, error) {
			return entry, nil
		},
	}, &fakeServiceBlobStore{
		openFn: func(context.Context, string, *storage.ByteRange) (storage.Object, error) {
			return storage.Object{Body: io.NopCloser(bytes.NewReader(payload)), Size: int64(len(payload))}, nil
		},
	})
	body, err := service.ReadSessionFile(context.Background(), filesystem.WorkspaceUUID, "sesn_score", "/outputs/score.json", 32)
	if err != nil || string(body) != string(payload) {
		t.Fatalf("body = (%s, %v)", body, err)
	}
}

func TestWriteSessionFileCreatesParentBeforeUpload(t *testing.T) {
	filesystem := serviceTestFilesystem()
	var directory string
	service := newServiceUnderTest(filestoreTestConfig(100, 0, "filestore-test"), &fakeServiceDatabase{
		getSessionFilesystemFn: func(context.Context, string, string) (db.FilestoreFilesystem, error) {
			return filesystem, nil
		},
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		makeDirectoryFn: func(_ context.Context, input db.MakeFilestoreDirectoryInput) (db.SessionResourceFile, error) {
			directory = input.Path
			if !input.MakeParents {
				t.Fatal("parents were not requested")
			}
			return serviceTestDirectoryEntry(filesystem, 7, input.Path), nil
		},
		getEntryFn: func(_ context.Context, _, _, entryPath string) (db.SessionResourceFile, error) {
			if entryPath != "/uploads/data/session" {
				return db.SessionResourceFile{}, db.ErrNotFound
			}
			return serviceTestDirectoryEntry(filesystem, 7, entryPath), nil
		},
		enqueueCleanupFn: func(context.Context, db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			return db.FilestoreObjectCleanupJob{UUID: "00000000-0000-4000-8000-000000000089"}, nil
		},
		completeCleanupFn: func(context.Context, string) error { return nil },
	}, &fakeServiceBlobStore{
		uploadFn: func(context.Context, string, io.Reader, storage.UploadOptions) (storage.UploadResult, error) {
			return storage.UploadResult{}, errors.New("upload failed")
		},
		deleteFn: func(context.Context, string, storage.DeleteOptions) error { return nil },
	})
	err := service.WriteSessionFile(context.Background(), filesystem.WorkspaceUUID, "sesn_score", "/uploads/data/session", "/uploads/data/session/result.json", "application/json", []byte(`{"output":"hi"}`))
	if err == nil || directory != "/uploads/data/session" {
		t.Fatalf("write error = %v directory = %q", err, directory)
	}
}
