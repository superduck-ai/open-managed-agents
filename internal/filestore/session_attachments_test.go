package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func sessionAttachmentTestDatabase(filesystem db.FilestoreFilesystem, put func(db.PutFilestoreFileInput) error) (*fakeServiceDatabase, *db.MakeFilestoreDirectoryInput) {
	var directory db.MakeFilestoreDirectoryInput
	return &fakeServiceDatabase{
		getSessionFilesystemFn: func(_ context.Context, workspaceUUID, sessionExternalID string) (db.FilestoreFilesystem, error) {
			if workspaceUUID != filesystem.WorkspaceUUID || sessionExternalID != "sesn_attachments" {
				return db.FilestoreFilesystem{}, db.ErrNotFound
			}
			return filesystem, nil
		},
		makeDirectoryFn: func(_ context.Context, input db.MakeFilestoreDirectoryInput) (db.SessionResourceFile, error) {
			directory = input
			return db.SessionResourceFile{}, nil
		},
		enqueueCleanupFn: func(context.Context, db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			return db.FilestoreObjectCleanupJob{ExternalID: "cleanup_attachment"}, nil
		},
		attachCleanupFn: func(context.Context, string, string, string, string) error { return nil },
		putFileFn: func(_ context.Context, input db.PutFilestoreFileInput) (db.FilestoreMutationResult, error) {
			return db.FilestoreMutationResult{}, put(input)
		},
	}, &directory
}

func sessionAttachmentTarget(filesystem db.FilestoreFilesystem) SessionAttachmentTarget {
	return SessionAttachmentTarget{
		WorkspaceUUID:     filesystem.WorkspaceUUID,
		SessionExternalID: "sesn_attachments",
		Path:              "/uploads/message_attachments/sevt_1/0.png",
	}
}

func uploadingBlobStore(uploaded *[]byte) *fakeServiceBlobStore {
	return &fakeServiceBlobStore{
		uploadFn: func(_ context.Context, _ string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
			data, err := io.ReadAll(body)
			*uploaded = data
			return storage.UploadResult{Size: int64(len(data)), ETag: "etag", VersionID: "version"}, err
		},
	}
}

func TestPutSessionAttachmentFailsWithoutSessionFilesystem(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	database, _ := sessionAttachmentTestDatabase(filesystem, func(db.PutFilestoreFileInput) error { return nil })
	target := sessionAttachmentTarget(filesystem)
	target.SessionExternalID = "sesn_missing"
	service := newServiceUnderTest(filestoreTestConfig(1024, 0, "filestore-test"), database, &fakeServiceBlobStore{})

	err := service.PutSessionAttachment(context.Background(), target, "image/png", strings.NewReader("png"))
	if !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("PutSessionAttachment() error = %v, want not found", err)
	}
}

func TestPutSessionAttachmentPropagatesRecordFailure(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	failure := errors.New("commit failed")
	database, _ := sessionAttachmentTestDatabase(filesystem, func(db.PutFilestoreFileInput) error { return failure })
	var uploaded []byte
	service := newServiceUnderTest(filestoreTestConfig(1024, 0, "filestore-test"), database, uploadingBlobStore(&uploaded))

	err := service.PutSessionAttachment(context.Background(), sessionAttachmentTarget(filesystem), "image/png", strings.NewReader("png"))
	if !errors.Is(err, failure) {
		t.Fatalf("PutSessionAttachment() error = %v, want %v", err, failure)
	}
}

func TestPutSessionAttachmentTreatsExistingPathAsAlreadyStored(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	database, _ := sessionAttachmentTestDatabase(filesystem, func(db.PutFilestoreFileInput) error { return db.ErrFilestorePathExists })
	var uploaded []byte
	service := newServiceUnderTest(filestoreTestConfig(1024, 0, "filestore-test"), database, uploadingBlobStore(&uploaded))

	if err := service.PutSessionAttachment(context.Background(), sessionAttachmentTarget(filesystem), "image/png", strings.NewReader("png")); err != nil {
		t.Fatalf("PutSessionAttachment() error = %v", err)
	}
}

func TestPutSessionAttachmentRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	recorded := false
	database, _ := sessionAttachmentTestDatabase(filesystem, func(db.PutFilestoreFileInput) error {
		recorded = true
		return nil
	})
	database.completeCleanupFn = func(context.Context, string) error { return nil }
	var uploaded []byte
	store := uploadingBlobStore(&uploaded)
	discarded := false
	store.deleteFn = func(context.Context, string, storage.DeleteOptions) error {
		discarded = true
		return nil
	}
	service := newServiceUnderTest(filestoreTestConfig(4, 0, "filestore-test"), database, store)

	err := service.PutSessionAttachment(context.Background(), sessionAttachmentTarget(filesystem), "image/png", strings.NewReader("png-bytes"))
	if !errors.Is(err, ErrSessionAttachmentRejected) || recorded || !discarded {
		t.Fatalf("PutSessionAttachment() error = %v, recorded = %t, discarded = %t", err, recorded, discarded)
	}
}

func TestCopyFileToSessionAttachmentRejectsMismatchedContent(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	recorded := false
	database, _ := sessionAttachmentTestDatabase(filesystem, func(db.PutFilestoreFileInput) error {
		recorded = true
		return nil
	})
	var uploaded []byte
	store := uploadingBlobStore(&uploaded)
	store.openFn = func(context.Context, string, *storage.ByteRange) (storage.Object, error) {
		return storage.Object{Body: io.NopCloser(strings.NewReader("plain text, not a pdf"))}, nil
	}
	service := newServiceUnderTest(filestoreTestConfig(1024, 0, "filestore-test"), database, store)

	err := service.CopyFileToSessionAttachment(context.Background(), sessionAttachmentTarget(filesystem), db.FileRecord{S3Key: "source", MimeType: "application/pdf"})
	if !errors.Is(err, ErrSessionAttachmentRejected) || recorded || uploaded != nil {
		t.Fatalf("CopyFileToSessionAttachment() error = %v, recorded = %t, uploaded = %q", err, recorded, uploaded)
	}
}

func TestPutSessionAttachmentStoresHiddenOwnedFile(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	var putInput db.PutFilestoreFileInput
	database, directory := sessionAttachmentTestDatabase(filesystem, func(input db.PutFilestoreFileInput) error {
		putInput = input
		return nil
	})
	var uploaded []byte
	service := newServiceUnderTest(filestoreTestConfig(1024, 4096, "filestore-test"), database, uploadingBlobStore(&uploaded))
	target := sessionAttachmentTarget(filesystem)

	if err := service.PutSessionAttachment(context.Background(), target, "Image/PNG", strings.NewReader("png-bytes")); err != nil {
		t.Fatalf("PutSessionAttachment() error = %v", err)
	}
	if directory.Path != "/uploads/message_attachments/sevt_1" || !directory.MakeParents || directory.FilesystemUUID != filesystem.UUID {
		t.Fatalf("directory input = %+v", *directory)
	}
	sum := sha256.Sum256([]byte("png-bytes"))
	if string(uploaded) != "png-bytes" || putInput.Path != target.Path || putInput.OverwriteExisting ||
		putInput.Blob.MediaType != "image/png" || putInput.Blob.SizeBytes != 9 || putInput.Blob.SHA256 != hex.EncodeToString(sum[:]) ||
		putInput.Blob.Downloadable || putInput.WorkspaceStorageLimitBytes != 0 || putInput.OrphanCleanupJobExternalID != "cleanup_attachment" {
		t.Fatalf("put input = %+v, uploaded %q", putInput, uploaded)
	}
}

func TestCopyFileToSessionAttachmentStreamsSourceObject(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	var putInput db.PutFilestoreFileInput
	database, _ := sessionAttachmentTestDatabase(filesystem, func(input db.PutFilestoreFileInput) error {
		putInput = input
		return nil
	})
	var uploaded []byte
	store := uploadingBlobStore(&uploaded)
	store.openFn = func(_ context.Context, key string, _ *storage.ByteRange) (storage.Object, error) {
		if key != "workspaces/source/report.pdf" {
			return storage.Object{}, errors.New("unexpected key")
		}
		return storage.Object{Body: io.NopCloser(strings.NewReader("%PDF-1.4"))}, nil
	}
	service := newServiceUnderTest(filestoreTestConfig(1024, 0, "filestore-test"), database, store)

	err := service.CopyFileToSessionAttachment(context.Background(), sessionAttachmentTarget(filesystem), db.FileRecord{
		S3Key: "workspaces/source/report.pdf", MimeType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("CopyFileToSessionAttachment() error = %v", err)
	}
	if string(uploaded) != "%PDF-1.4" || putInput.Blob.MediaType != "application/pdf" {
		t.Fatalf("copied body = %q, put input = %+v", uploaded, putInput)
	}
}
