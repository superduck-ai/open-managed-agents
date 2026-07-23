package filestore

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
)

func newServiceUnderTest(cfg config.Config, database filestoreDatabase, store storage.ObjectStore) *Service {
	service := NewService(cfg, database, store)
	service.now = func() time.Time { return serviceTestNow }
	return service
}

func filestoreTestConfig(maxFileBytes, workspaceLimitBytes int64, bucket string) config.Config {
	return config.Config{Storage: config.StorageConfig{
		MaxFileBytes:        maxFileBytes,
		WorkspaceLimitBytes: workspaceLimitBytes,
		S3:                  config.S3Config{Bucket: bucket},
	}}
}

func serviceTestPrincipal() Principal {
	return Principal{
		OrganizationID:       11,
		OrganizationUUID:     "11111111-1111-4111-8111-111111111111",
		WorkspaceID:          22,
		WorkspaceUUID:        "22222222-2222-4222-8222-222222222222",
		FilesystemInternalID: 55,
		FilesystemUUID:       "55555555-5555-4555-8555-555555555555",
		FilesystemExternalID: "fs_test",
	}
}

func serviceTestFilesystem() db.FilestoreFilesystem {
	return db.FilestoreFilesystem{
		ID:               55,
		UUID:             "55555555-5555-4555-8555-555555555555",
		ExternalID:       "fs_test",
		OrganizationUUID: serviceTestPrincipal().OrganizationUUID,
		WorkspaceUUID:    serviceTestPrincipal().WorkspaceUUID,
		SessionUUID:      "33333333-3333-4333-8333-333333333333",
		CreatedAt:        serviceTestNow.Add(-time.Hour),
		UpdatedAt:        serviceTestNow.Add(-time.Hour),
	}
}

func serviceTestDirectoryEntry(filesystem db.FilestoreFilesystem, id int64, entryPath string) db.FilestoreEntry {
	return db.FilestoreEntry{
		ID:               id,
		UUID:             uuid.NewString(),
		ExternalID:       "dir_test",
		OrganizationUUID: serviceTestPrincipal().OrganizationUUID,
		WorkspaceUUID:    serviceTestPrincipal().WorkspaceUUID,
		FilesystemUUID:   filesystem.UUID,
		Kind:             db.FilestoreEntryKindDirectory,
		Path:             entryPath,
		CreatedAt:        serviceTestNow.Add(-time.Minute),
		UpdatedAt:        serviceTestNow.Add(-time.Minute),
	}
}

func serviceTestFileEntry(filesystem db.FilestoreFilesystem, entryPath string, contents []byte) db.FilestoreEntry {
	md5Sum := md5.Sum(contents)
	sha256Sum := sha256.Sum256(contents)
	return serviceTestFileEntryFromBlob(filesystem, "file_test", entryPath, db.FilestoreFileBlob{
		SizeBytes:             int64(len(contents)),
		MediaType:             "text/plain",
		DetectedMimeType:      "text/plain",
		Metadata:              json.RawMessage("{}"),
		AuthorizationMetadata: json.RawMessage("{}"),
		Downloadable:          true,
		MD5:                   hex.EncodeToString(md5Sum[:]),
		SHA256:                hex.EncodeToString(sha256Sum[:]),
		S3Bucket:              "filestore-test",
		S3Key:                 "workspaces/22/filestores/55/blobs/file-test",
		S3ETag:                "etag-source",
		S3VersionID:           "version-source",
	})
}

func serviceTestFileEntryFromBlob(filesystem db.FilestoreFilesystem, externalID, entryPath string, blob db.FilestoreFileBlob) db.FilestoreEntry {
	return db.FilestoreEntry{
		ID:                    60,
		UUID:                  uuid.NewString(),
		ExternalID:            externalID,
		OrganizationUUID:      serviceTestPrincipal().OrganizationUUID,
		WorkspaceUUID:         serviceTestPrincipal().WorkspaceUUID,
		FilesystemUUID:        filesystem.UUID,
		Kind:                  db.FilestoreEntryKindFile,
		Path:                  entryPath,
		SizeBytes:             serviceTestPointer(blob.SizeBytes),
		MediaType:             serviceTestPointer(blob.MediaType),
		DetectedMimeType:      serviceTestPointer(blob.DetectedMimeType),
		Metadata:              append(json.RawMessage(nil), blob.Metadata...),
		AuthorizationMetadata: append(json.RawMessage(nil), blob.AuthorizationMetadata...),
		Tags:                  append([]string(nil), blob.Tags...),
		Downloadable:          blob.Downloadable,
		MD5:                   serviceTestPointer(blob.MD5),
		SHA256:                serviceTestPointer(blob.SHA256),
		S3Bucket:              serviceTestPointer(blob.S3Bucket),
		S3Key:                 serviceTestPointer(blob.S3Key),
		S3ETag:                serviceTestPointer(blob.S3ETag),
		S3VersionID:           serviceTestPointer(blob.S3VersionID),
		ExpiresAt:             blob.ExpiresAt,
		CreatedAt:             serviceTestNow,
		UpdatedAt:             serviceTestNow,
	}
}

func serviceFilesystemLookup(filesystem db.FilestoreFilesystem) func(context.Context, int64, string) (db.FilestoreFilesystem, error) {
	return func(_ context.Context, workspaceID int64, filesystemID string) (db.FilestoreFilesystem, error) {
		matchesExternalID := filesystemID == filesystem.ExternalID
		matchesUUID := strings.EqualFold(filesystemID, filesystem.UUID)
		if workspaceID != serviceTestPrincipal().WorkspaceID || (!matchesExternalID && !matchesUUID) {
			return db.FilestoreFilesystem{}, db.ErrNotFound
		}
		return filesystem, nil
	}
}

func serviceParentDirectoryLookup(filesystem db.FilestoreFilesystem) func(context.Context, int64, int64, string) (db.FilestoreEntry, error) {
	return func(_ context.Context, workspaceID, filesystemID int64, entryPath string) (db.FilestoreEntry, error) {
		if workspaceID != serviceTestPrincipal().WorkspaceID || filesystemID != filesystem.ID || entryPath != "/" {
			return db.FilestoreEntry{}, db.ErrNotFound
		}
		return serviceTestDirectoryEntry(filesystem, 1, "/"), nil
	}
}

func serviceTestPointer[T any](value T) *T {
	return &value
}

func assertCleanupEntryExternalIDMatchesBlobKey(t *testing.T, input db.EnqueueFilestoreObjectCleanupJobInput) {
	t.Helper()
	separator := strings.LastIndex(input.Key, "/")
	if separator < 0 {
		t.Fatalf("cleanup key = %q, want object path", input.Key)
	}
	blobUUID := input.Key[separator+1:]
	if _, err := uuid.Parse(blobUUID); err != nil {
		t.Fatalf("cleanup blob UUID = %q: %v", blobUUID, err)
	}
	if input.EntryExternalID != blobUUID {
		t.Fatalf("cleanup entry external ID = %q, want blob UUID %q", input.EntryExternalID, blobUUID)
	}
}

func assertServiceAPIError(t *testing.T, apiErr *apiError, status int, code string) {
	t.Helper()
	if apiErr == nil {
		t.Fatalf("error = nil, want status %d code %q", status, code)
	}
	if apiErr.Status != status || apiErr.Code != code {
		t.Fatalf("error = %+v, want status %d code %q", apiErr, status, code)
	}
}

type fakeServiceDatabase struct {
	filestoreDatabase
	getFilesystemFn   func(context.Context, int64, string) (db.FilestoreFilesystem, error)
	getEntryFn        func(context.Context, int64, int64, string) (db.FilestoreEntry, error)
	listEntriesFn     func(context.Context, db.ListFilestoreEntriesPageParams) (db.FilestoreEntryPage, error)
	putFileFn         func(context.Context, db.PutFilestoreFileInput) (db.FilestoreMutationResult, error)
	copyFileFn        func(context.Context, db.CopyFilestoreFileInput) (db.FilestoreMutationResult, error)
	moveFileFn        func(context.Context, db.MoveFilestoreFileInput) (db.FilestoreMutationResult, error)
	moveDirectoryFn   func(context.Context, db.MoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error)
	removeFileFn      func(context.Context, db.RemoveFilestoreEntryInput) (db.FilestoreMutationResult, error)
	removeDirectoryFn func(context.Context, db.RemoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error)
	enqueueCleanupFn  func(context.Context, db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error)
	attachCleanupFn   func(context.Context, int64, string, string, string) error
	completeCleanupFn func(context.Context, int64) error
}

func (f *fakeServiceDatabase) GetFilestoreFilesystem(ctx context.Context, workspaceID int64, externalID string) (db.FilestoreFilesystem, error) {
	if f.getFilesystemFn == nil {
		panic("unexpected GetFilestoreFilesystem call")
	}
	return f.getFilesystemFn(ctx, workspaceID, externalID)
}

func (f *fakeServiceDatabase) GetFilestoreEntry(ctx context.Context, workspaceID, filesystemID int64, entryPath string) (db.FilestoreEntry, error) {
	if f.getEntryFn == nil {
		panic("unexpected GetFilestoreEntry call")
	}
	return f.getEntryFn(ctx, workspaceID, filesystemID, entryPath)
}

func (f *fakeServiceDatabase) ListFilestoreEntriesPage(ctx context.Context, input db.ListFilestoreEntriesPageParams) (db.FilestoreEntryPage, error) {
	if f.listEntriesFn == nil {
		panic("unexpected ListFilestoreEntriesPage call")
	}
	return f.listEntriesFn(ctx, input)
}

func (f *fakeServiceDatabase) PutFilestoreFile(ctx context.Context, input db.PutFilestoreFileInput) (db.FilestoreMutationResult, error) {
	if f.putFileFn == nil {
		panic("unexpected PutFilestoreFile call")
	}
	return f.putFileFn(ctx, input)
}

func (f *fakeServiceDatabase) CopyFilestoreFile(ctx context.Context, input db.CopyFilestoreFileInput) (db.FilestoreMutationResult, error) {
	if f.copyFileFn == nil {
		panic("unexpected CopyFilestoreFile call")
	}
	return f.copyFileFn(ctx, input)
}

func (f *fakeServiceDatabase) MoveFilestoreFile(ctx context.Context, input db.MoveFilestoreFileInput) (db.FilestoreMutationResult, error) {
	if f.moveFileFn == nil {
		panic("unexpected MoveFilestoreFile call")
	}
	return f.moveFileFn(ctx, input)
}

func (f *fakeServiceDatabase) MoveFilestoreDirectory(ctx context.Context, input db.MoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error) {
	if f.moveDirectoryFn == nil {
		panic("unexpected MoveFilestoreDirectory call")
	}
	return f.moveDirectoryFn(ctx, input)
}

func (f *fakeServiceDatabase) RemoveFilestoreFile(ctx context.Context, input db.RemoveFilestoreEntryInput) (db.FilestoreMutationResult, error) {
	if f.removeFileFn == nil {
		panic("unexpected RemoveFilestoreFile call")
	}
	return f.removeFileFn(ctx, input)
}

func (f *fakeServiceDatabase) RemoveFilestoreDirectory(ctx context.Context, input db.RemoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error) {
	if f.removeDirectoryFn == nil {
		panic("unexpected RemoveFilestoreDirectory call")
	}
	return f.removeDirectoryFn(ctx, input)
}

func (f *fakeServiceDatabase) EnqueueFilestoreObjectCleanupJob(ctx context.Context, input db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
	if f.enqueueCleanupFn == nil {
		panic("unexpected EnqueueFilestoreObjectCleanupJob call")
	}
	return f.enqueueCleanupFn(ctx, input)
}

func (f *fakeServiceDatabase) AttachFilestoreObjectCleanupJobVersion(ctx context.Context, workspaceID int64, jobExternalID, etag, versionID string) error {
	if f.attachCleanupFn == nil {
		panic("unexpected AttachFilestoreObjectCleanupJobVersion call")
	}
	return f.attachCleanupFn(ctx, workspaceID, jobExternalID, etag, versionID)
}

func (f *fakeServiceDatabase) CompleteFilestoreObjectCleanupJob(ctx context.Context, jobID int64) error {
	if f.completeCleanupFn == nil {
		panic("unexpected CompleteFilestoreObjectCleanupJob call")
	}
	return f.completeCleanupFn(ctx, jobID)
}

type fakeServiceBlobStore struct {
	uploadFn func(context.Context, string, io.Reader, storage.UploadOptions) (storage.UploadResult, error)
	openFn   func(context.Context, string, *storage.ByteRange) (storage.Object, error)
	copyFn   func(context.Context, string, string) (storage.CopyResult, error)
	deleteFn func(context.Context, string, storage.DeleteOptions) error
}

func (*fakeServiceBlobStore) Ensure(context.Context) error { return nil }

func (*fakeServiceBlobStore) Name() string { return "filestore-test" }

func (f *fakeServiceBlobStore) Upload(ctx context.Context, key string, body io.Reader, options storage.UploadOptions) (storage.UploadResult, error) {
	if f.uploadFn == nil {
		panic("unexpected Upload call")
	}
	return f.uploadFn(ctx, key, body, options)
}

func (f *fakeServiceBlobStore) Open(ctx context.Context, key string, byteRange *storage.ByteRange) (storage.Object, error) {
	if f.openFn == nil {
		panic("unexpected Open call")
	}
	return f.openFn(ctx, key, byteRange)
}

func (f *fakeServiceBlobStore) Copy(ctx context.Context, sourceKey, destinationKey string) (storage.CopyResult, error) {
	if f.copyFn == nil {
		panic("unexpected Copy call")
	}
	return f.copyFn(ctx, sourceKey, destinationKey)
}

func (f *fakeServiceBlobStore) Delete(ctx context.Context, key string, options storage.DeleteOptions) error {
	if f.deleteFn == nil {
		panic("unexpected Delete call")
	}
	return f.deleteFn(ctx, key, options)
}
