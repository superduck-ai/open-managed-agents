package evaluations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestWorkspaceFileCopierCopiesBytesIntoWorkspaceFile(t *testing.T) {
	records := &fakeCaseFileRecords{files: map[string]db.FileRecord{
		"file_in": {
			ExternalID: "file_in", WorkspaceUUID: "workspace-uuid", Filename: "brief.txt",
			MimeType: "text/plain", SizeBytes: 4, SHA256: "abcd", S3Bucket: "files", S3Key: "workspaces/workspace-uuid/filestores/fs/blobs/src",
			Downloadable: true,
		},
	}}
	objects := &fakeCaseFileObjects{name: "files"}
	copier := &WorkspaceFileCopier{records: records, objects: objects, maxFileBytes: 10, workspaceLimitBytes: 100}
	copied, err := copier.CopyCaseFile(context.Background(), "workspace-uuid", sessionCaseFileRef{Name: "/uploads/brief.txt", FileID: "file_in"})
	if err != nil || !strings.HasPrefix(copied.FileID, "file_") || copied.FileID == "file_in" || copied.Name != "/uploads/brief.txt" {
		t.Fatalf("copied = %#v err=%v", copied, err)
	}
	if objects.source != records.files["file_in"].S3Key || !strings.HasPrefix(objects.destination, "workspaces/workspace-uuid/files/") || !strings.HasSuffix(objects.destination, "/brief.txt") {
		t.Fatalf("copy %s -> %s", objects.source, objects.destination)
	}
	if records.created.ExternalID != copied.FileID || records.created.S3Key != objects.destination || records.created.SizeBytes != 4 || records.created.SHA256 != "abcd" || records.created.MimeType != "text/plain" || !records.created.Downloadable || records.created.ScopeType != nil {
		t.Fatalf("record = %#v", records.created)
	}
}

func TestWorkspaceFileCopierSkipsMissingAndCleansFailedMetadata(t *testing.T) {
	records := &fakeCaseFileRecords{}
	objects := &fakeCaseFileObjects{name: "files"}
	copier := &WorkspaceFileCopier{records: records, objects: objects, maxFileBytes: 10, workspaceLimitBytes: 100}
	if _, err := copier.CopyCaseFile(context.Background(), "workspace-uuid", sessionCaseFileRef{Name: "gone.txt", FileID: "file_missing"}); !errors.Is(err, errCaseFileNotFound) || objects.source != "" {
		t.Fatalf("missing err=%v copied=%s", err, objects.source)
	}
	records.files = map[string]db.FileRecord{
		"file_big":   {ExternalID: "file_big", SizeBytes: 11, S3Bucket: "files", S3Key: "src"},
		"file_other": {ExternalID: "file_other", SizeBytes: 1, S3Bucket: "other", S3Key: "src"},
	}
	if _, err := copier.CopyCaseFile(context.Background(), "workspace-uuid", sessionCaseFileRef{FileID: "file_big"}); !errors.Is(err, errCaseFileTooLarge) || objects.source != "" {
		t.Fatalf("large err=%v", err)
	}
	if _, err := copier.CopyCaseFile(context.Background(), "workspace-uuid", sessionCaseFileRef{FileID: "file_other"}); !errors.Is(err, errCaseFileBucket) || objects.source != "" {
		t.Fatalf("bucket err=%v", err)
	}
	records.files["file_in"] = db.FileRecord{ExternalID: "file_in", SizeBytes: 1, S3Bucket: "files", S3Key: "src", Filename: "brief.txt"}
	records.createErr = errors.New("insert failed")
	if _, err := copier.CopyCaseFile(context.Background(), "workspace-uuid", sessionCaseFileRef{Name: "brief.txt", FileID: "file_in"}); !errors.Is(err, errCaseFileCopy) || objects.deleted != objects.destination {
		t.Fatalf("cleanup err=%v deleted=%s destination=%s", err, objects.deleted, objects.destination)
	}
	records.createErr = db.ErrStorageLimitExceeded
	if _, err := copier.CopyCaseFile(context.Background(), "workspace-uuid", sessionCaseFileRef{Name: "brief.txt", FileID: "file_in"}); !errors.Is(err, errCaseFileStorage) {
		t.Fatalf("limit err=%v", err)
	}
}

type fakeCaseFileRecords struct {
	files     map[string]db.FileRecord
	created   db.FileRecord
	createErr error
}

func (f *fakeCaseFileRecords) GetFile(_ context.Context, _, fileExternalID string) (db.FileRecord, error) {
	record, ok := f.files[fileExternalID]
	if !ok {
		return db.FileRecord{}, db.ErrNotFound
	}
	return record, nil
}

func (f *fakeCaseFileRecords) CreateFileIfWithinLimit(_ context.Context, file db.FileRecord, _ int64) error {
	f.created = file
	return f.createErr
}

type fakeCaseFileObjects struct {
	name        string
	source      string
	destination string
	deleted     string
}

func (f *fakeCaseFileObjects) Name() string { return f.name }

func (f *fakeCaseFileObjects) Copy(_ context.Context, sourceKey, destinationKey string) (storage.CopyResult, error) {
	f.source = sourceKey
	f.destination = destinationKey
	return storage.CopyResult{}, nil
}

func (f *fakeCaseFileObjects) Delete(_ context.Context, key string, _ storage.DeleteOptions) error {
	f.deleted = key
	return nil
}
