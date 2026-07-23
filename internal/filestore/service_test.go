package filestore

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"

	"github.com/google/uuid"
)

var serviceTestNow = time.Date(2026, time.July, 21, 12, 30, 0, 123456789, time.UTC)

func TestServiceRejectsInvalidPathsBeforeDependencies(t *testing.T) {
	t.Parallel()

	principal := serviceTestPrincipal()
	tests := []struct {
		name string
		run  func(*Service) *apiError
	}{
		{
			name: "invalid filesystem identifier",
			run: func(service *Service) *apiError {
				_, apiErr := service.ListDirectory(context.Background(), principal, listDirectoryRequest{FilesystemID: "invalid", Path: "/"})
				return apiErr
			},
		},
		{
			name: "relative directory path",
			run: func(service *Service) *apiError {
				_, apiErr := service.MakeDirectory(context.Background(), principal, makeDirectoryRequest{FilesystemID: "fs_test", Path: "relative"})
				return apiErr
			},
		},
		{
			name: "create at root",
			run: func(service *Service) *apiError {
				_, apiErr := service.CreateFile(context.Background(), principal, createFileParams{FilesystemID: "fs_test", Path: "/", MediaType: "text/plain"}, strings.NewReader("body"))
				return apiErr
			},
		},
		{
			name: "copy destination with dot segment",
			run: func(service *Service) *apiError {
				_, apiErr := service.CopyFile(context.Background(), principal, copyMoveFileRequest{FilesystemID: "fs_test", Source: "/source", Destination: "/archive/../copy"})
				return apiErr
			},
		},
		{
			name: "move directory into itself",
			run: func(service *Service) *apiError {
				_, apiErr := service.MoveDirectory(context.Background(), principal, moveDirectoryRequest{FilesystemID: "fs_test", Source: "/source", Destination: "/source/child"})
				return apiErr
			},
		},
		{
			name: "remove root file",
			run: func(service *Service) *apiError {
				return service.RemoveFile(context.Background(), principal, pathRequest{FilesystemID: "fs_test", Path: "/"})
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := newServiceUnderTest(config.Config{}, &fakeServiceDatabase{}, &fakeServiceBlobStore{})
			assertServiceAPIError(t, test.run(service), http.StatusBadRequest, "invalid_argument")
		})
	}
}

func TestServiceFilestoreTokenBindsSingleFilesystem(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	for _, test := range []struct {
		name       string
		requestID  string
		internalID int64
		database   *fakeServiceDatabase
	}{
		{
			name:       "reject another filesystem identifier",
			requestID:  "fs_other",
			internalID: filesystem.ID,
			database:   &fakeServiceDatabase{},
		},
		{
			name:       "reject stale internal binding",
			requestID:  filesystem.ExternalID,
			internalID: filesystem.ID + 1,
			database: &fakeServiceDatabase{
				getFilesystemFn: serviceFilesystemLookup(filesystem),
			},
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			principal := serviceTestPrincipal()
			principal.FilesystemUUID = filesystem.UUID
			principal.FilesystemExternalID = filesystem.ExternalID
			principal.FilesystemInternalID = test.internalID
			service := newServiceUnderTest(config.Config{}, test.database, &fakeServiceBlobStore{})

			_, apiErr := service.resolveFilesystem(context.Background(), principal, test.requestID)

			assertServiceAPIError(t, apiErr, http.StatusForbidden, "permission_denied")
		})
	}

	principal := serviceTestPrincipal()
	principal.FilesystemUUID = filesystem.UUID
	principal.FilesystemExternalID = filesystem.ExternalID
	principal.FilesystemInternalID = filesystem.ID
	service := newServiceUnderTest(config.Config{}, &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
	}, &fakeServiceBlobStore{})

	for _, requestID := range []string{filesystem.ExternalID, strings.ToUpper(filesystem.UUID)} {
		got, apiErr := service.resolveFilesystem(context.Background(), principal, requestID)

		if apiErr != nil {
			t.Fatalf("resolveFilesystem(%q) error = %v", requestID, apiErr)
		}
		if got.ID != filesystem.ID {
			t.Fatalf("resolveFilesystem(%q) = %#v, want ID %d", requestID, got, filesystem.ID)
		}
	}
}

func TestServiceListDirectoryRejectsInvalidLimitAndCursor(t *testing.T) {
	t.Parallel()

	mismatchedCursor, err := encodeDirectoryCursor(directoryCursor{
		FilesystemID: "fs_other",
		Path:         "/reports",
		LastPath:     "/reports/old",
		LastID:       10,
	})
	if err != nil {
		t.Fatalf("encode mismatched cursor: %v", err)
	}
	tests := []struct {
		name    string
		request listDirectoryRequest
	}{
		{name: "negative limit", request: listDirectoryRequest{FilesystemID: "fs_test", Path: "/", Limit: -1}},
		{name: "limit above maximum", request: listDirectoryRequest{FilesystemID: "fs_test", Path: "/", Limit: 1001}},
		{name: "malformed cursor", request: listDirectoryRequest{FilesystemID: "fs_test", Path: "/", Cursor: "not-base64%%%"}},
		{name: "cursor for another filesystem", request: listDirectoryRequest{FilesystemID: "fs_test", Path: "/reports", Cursor: mismatchedCursor}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := newServiceUnderTest(config.Config{}, &fakeServiceDatabase{}, &fakeServiceBlobStore{})
			_, apiErr := service.ListDirectory(context.Background(), serviceTestPrincipal(), test.request)
			assertServiceAPIError(t, apiErr, http.StatusBadRequest, "invalid_argument")
		})
	}
}

func TestServiceReadFileRejectsInvalidRangesBeforeObjectLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		requestRange readFileRange
	}{
		{name: "negative offset", requestRange: readFileRange{Offset: -1, Length: 1}},
		{name: "length below sentinel", requestRange: readFileRange{Offset: 0, Length: -2}},
		{name: "offset beyond file", requestRange: readFileRange{Offset: 6, Length: -1}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filesystem := serviceTestFilesystem()
			database := &fakeServiceDatabase{
				getFilesystemFn: func(context.Context, int64, string) (db.FilestoreFilesystem, error) {
					return filesystem, nil
				},
				getEntryFn: func(context.Context, int64, int64, string) (db.FilestoreEntry, error) {
					return serviceTestFileEntry(filesystem, "/file.txt", []byte("12345")), nil
				},
			}
			service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
			_, apiErr := service.ReadFile(context.Background(), serviceTestPrincipal(), readFileRequest{
				FilesystemID: filesystem.ExternalID,
				Path:         "/file.txt",
				Range:        &test.requestRange,
			})

			assertServiceAPIError(t, apiErr, http.StatusRequestedRangeNotSatisfiable, "invalid_argument")
		})
	}
}

func TestMapBlobstoreAccessDeniedPreservesCauseBehindUnavailableResponse(t *testing.T) {
	t.Parallel()

	storeErr := errors.Join(errors.New("signature rejected"), storage.ErrAccessDenied)
	apiErr := mapBlobstoreError("read file", storeErr)

	assertServiceAPIError(t, apiErr, http.StatusServiceUnavailable, "unavailable")
	if !errors.Is(apiErr, storage.ErrAccessDenied) {
		t.Fatalf("error = %v, want access-denied cause", apiErr)
	}
}

func TestServiceCreateFileDiscardsOrphanGuardWhenUploadFails(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	job := db.FilestoreObjectCleanupJob{ID: 89, ExternalID: "cleanup_upload_failure"}
	var enqueueInput db.EnqueueFilestoreObjectCleanupJobInput
	var deletedKey string
	var deletedOptions storage.DeleteOptions
	var completedJobID int64
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn:      serviceParentDirectoryLookup(filesystem),
		enqueueCleanupFn: func(_ context.Context, input db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			enqueueInput = input
			return job, nil
		},
		completeCleanupFn: func(_ context.Context, jobID int64) error {
			completedJobID = jobID
			return nil
		},
	}
	store := &fakeServiceBlobStore{
		uploadFn: func(context.Context, string, io.Reader, storage.UploadOptions) (storage.UploadResult, error) {
			return storage.UploadResult{}, errors.New("upload result unknown")
		},
		deleteFn: func(_ context.Context, key string, options storage.DeleteOptions) error {
			deletedKey = key
			deletedOptions = options
			return nil
		},
	}
	service := newServiceUnderTest(filestoreTestConfig(100, 0, "filestore-test"), database, store)
	_, apiErr := service.CreateFile(context.Background(), serviceTestPrincipal(), createFileParams{
		FilesystemID: filesystem.ExternalID,
		Path:         "/failed.txt",
		MediaType:    "text/plain",
	}, strings.NewReader("payload"))

	assertServiceAPIError(t, apiErr, http.StatusServiceUnavailable, "unavailable")
	if enqueueInput.Key == "" || deletedKey != enqueueInput.Key || deletedOptions.VersionID != "" || !deletedOptions.AllVersions {
		t.Fatalf("enqueued key = %q, deleted key/options = %q/%+v", enqueueInput.Key, deletedKey, deletedOptions)
	}
	if completedJobID != job.ID {
		t.Fatalf("completed cleanup job = %d, want %d", completedJobID, job.ID)
	}
}

func TestServiceCopyFileDiscardsOrphanGuardWhenCopyFails(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	source := serviceTestFileEntry(filesystem, "/source.txt", []byte("source"))
	job := db.FilestoreObjectCleanupJob{ID: 90, ExternalID: "cleanup_copy_failure"}
	var enqueueInput db.EnqueueFilestoreObjectCleanupJobInput
	var deletedKey string
	var deletedOptions storage.DeleteOptions
	var completedJobID int64
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(_ context.Context, _ int64, _ int64, entryPath string) (db.FilestoreEntry, error) {
			switch entryPath {
			case "/source.txt":
				return source, nil
			case "/archive":
				return serviceTestDirectoryEntry(filesystem, 30, "/archive"), nil
			default:
				return db.FilestoreEntry{}, db.ErrNotFound
			}
		},
		enqueueCleanupFn: func(_ context.Context, input db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			enqueueInput = input
			return job, nil
		},
		completeCleanupFn: func(_ context.Context, jobID int64) error {
			completedJobID = jobID
			return nil
		},
	}
	store := &fakeServiceBlobStore{
		copyFn: func(context.Context, string, string) (storage.CopyResult, error) {
			return storage.CopyResult{}, errors.New("copy result unknown")
		},
		deleteFn: func(_ context.Context, key string, options storage.DeleteOptions) error {
			deletedKey = key
			deletedOptions = options
			return nil
		},
	}
	service := newServiceUnderTest(filestoreTestConfig(0, 0, "filestore-test"), database, store)
	_, apiErr := service.CopyFile(context.Background(), serviceTestPrincipal(), copyMoveFileRequest{
		FilesystemID: filesystem.ExternalID,
		Source:       "/source.txt",
		Destination:  "/archive/copied.txt",
	})

	assertServiceAPIError(t, apiErr, http.StatusServiceUnavailable, "unavailable")
	if enqueueInput.Key == "" || deletedKey != enqueueInput.Key || deletedOptions.VersionID != "" || !deletedOptions.AllVersions {
		t.Fatalf("enqueued key = %q, deleted key/options = %q/%+v", enqueueInput.Key, deletedKey, deletedOptions)
	}
	assertCleanupEntryExternalIDMatchesBlobKey(t, enqueueInput)
	if completedJobID != job.ID {
		t.Fatalf("completed cleanup job = %d, want %d", completedJobID, job.ID)
	}
}

func TestServiceCreateFileRejectsOversizeUploadAndCleansOrphan(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	job := db.FilestoreObjectCleanupJob{ID: 91, ExternalID: "cleanup_oversize"}
	var enqueued db.EnqueueFilestoreObjectCleanupJobInput
	var uploadedKey string
	var uploadedBody []byte
	var deletedKey string
	var deletedOptions storage.DeleteOptions
	var completedJobID int64
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn:      serviceParentDirectoryLookup(filesystem),
		enqueueCleanupFn: func(_ context.Context, input db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			enqueued = input
			return job, nil
		},
		completeCleanupFn: func(_ context.Context, jobID int64) error {
			completedJobID = jobID
			return nil
		},
	}
	store := &fakeServiceBlobStore{
		uploadFn: func(_ context.Context, key string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
			uploadedKey = key
			var err error
			uploadedBody, err = io.ReadAll(body)
			return storage.UploadResult{Size: int64(len(uploadedBody)), ETag: "etag-oversize", VersionID: "version-oversize"}, err
		},
		deleteFn: func(_ context.Context, key string, options storage.DeleteOptions) error {
			deletedKey = key
			deletedOptions = options
			return nil
		},
	}
	service := newServiceUnderTest(filestoreTestConfig(4, 0, "filestore-test"), database, store)
	_, apiErr := service.CreateFile(context.Background(), serviceTestPrincipal(), createFileParams{
		FilesystemID: filesystem.ExternalID,
		Path:         "/oversize.txt",
		MediaType:    "text/plain",
	}, strings.NewReader("123456789"))

	assertServiceAPIError(t, apiErr, http.StatusRequestEntityTooLarge, "resource_exhausted")
	if got := string(uploadedBody); got != "12345" {
		t.Fatalf("uploaded body = %q, want max+1 bytes", got)
	}
	if uploadedKey == "" || enqueued.Key != uploadedKey || deletedKey != uploadedKey || deletedOptions.VersionID != "version-oversize" || deletedOptions.AllVersions {
		t.Fatalf("uploaded key = %q, enqueued = %+v, deleted key/options = %q/%+v", uploadedKey, enqueued, deletedKey, deletedOptions)
	}
	if completedJobID != job.ID {
		t.Fatalf("completed cleanup job = %d, want %d", completedJobID, job.ID)
	}
}

func TestServiceCreateFileLeavesGuardWhenDatabaseCommitOutcomeIsUnknown(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	job := db.FilestoreObjectCleanupJob{ID: 92, ExternalID: "cleanup_commit_failure"}
	var uploadedKey string
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn:      serviceParentDirectoryLookup(filesystem),
		enqueueCleanupFn: func(context.Context, db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			return job, nil
		},
		putFileFn: func(context.Context, db.PutFilestoreFileInput) (db.FilestoreMutationResult, error) {
			return db.FilestoreMutationResult{}, errors.New("commit result unknown")
		},
		attachCleanupFn: func(context.Context, int64, string, string, string) error {
			return nil
		},
	}
	store := &fakeServiceBlobStore{
		uploadFn: func(_ context.Context, key string, body io.Reader, _ storage.UploadOptions) (storage.UploadResult, error) {
			uploadedKey = key
			data, err := io.ReadAll(body)
			return storage.UploadResult{Size: int64(len(data)), ETag: "etag-failed", VersionID: "version-failed"}, err
		},
	}
	service := newServiceUnderTest(filestoreTestConfig(100, 0, "filestore-test"), database, store)
	_, apiErr := service.CreateFile(context.Background(), serviceTestPrincipal(), createFileParams{
		FilesystemID: filesystem.ExternalID,
		Path:         "/limited.txt",
		MediaType:    "text/plain",
	}, strings.NewReader("payload"))

	assertServiceAPIError(t, apiErr, http.StatusInternalServerError, "internal")
	if uploadedKey == "" {
		t.Fatal("upload was not attempted")
	}
}

func TestServiceReadFileReturnsEmptyBodyWithoutObjectLookup(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(context.Context, int64, int64, string) (db.FilestoreEntry, error) {
			return serviceTestFileEntry(filesystem, "/empty-range.txt", []byte("12345")), nil
		},
	}
	service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
	result, apiErr := service.ReadFile(context.Background(), serviceTestPrincipal(), readFileRequest{
		FilesystemID: filesystem.ExternalID,
		Path:         "/empty-range.txt",
		Range:        &readFileRange{Offset: 5, Length: -1},
	})

	if apiErr != nil {
		t.Fatalf("ReadFile() error = %v", apiErr)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read empty result: %v", err)
	}
	if len(data) != 0 || result.Size != 0 || result.MediaType != "text/plain" {
		t.Fatalf("empty read result = size %d, media type %q, body %q", result.Size, result.MediaType, data)
	}
}

func TestServiceListDirectoryUsesBoundCursorAndReturnsNextCursor(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	requestCursor, err := encodeDirectoryCursor(directoryCursor{
		FilesystemID: filesystem.ExternalID,
		Path:         "/reports",
		Recursive:    true,
		LastPath:     "/reports/a",
		LastID:       10,
	})
	if err != nil {
		t.Fatalf("encode request cursor: %v", err)
	}
	entries := []db.FilestoreEntry{
		serviceTestDirectoryEntry(filesystem, 11, "/reports/b"),
		serviceTestDirectoryEntry(filesystem, 12, "/reports/c"),
	}
	var listInput db.ListFilestoreEntriesPageParams
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		listEntriesFn: func(_ context.Context, input db.ListFilestoreEntriesPageParams) (db.FilestoreEntryPage, error) {
			listInput = input
			return db.FilestoreEntryPage{Entries: entries, HasMore: true}, nil
		},
	}
	service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
	response, apiErr := service.ListDirectory(context.Background(), serviceTestPrincipal(), listDirectoryRequest{
		FilesystemID: filesystem.ExternalID,
		Path:         "/reports",
		Limit:        25,
		Cursor:       requestCursor,
		Recursive:    true,
	})

	if apiErr != nil {
		t.Fatalf("ListDirectory() error = %v", apiErr)
	}
	if listInput.WorkspaceID != serviceTestPrincipal().WorkspaceID || listInput.FilesystemID != filesystem.ID ||
		listInput.DirectoryPath != "/reports" || !listInput.Recursive || listInput.Limit != 25 || listInput.Cursor == nil ||
		listInput.Cursor.Path != "/reports/a" || listInput.Cursor.ID != 10 {
		t.Fatalf("list input = %+v", listInput)
	}
	if len(response.Entries) != 2 || response.Entries[0].Directory == nil || response.Entries[1].Directory == nil {
		t.Fatalf("response entries = %+v", response.Entries)
	}
	next, err := decodeDirectoryCursor(response.Cursor, filesystem.ExternalID, "/reports", true)
	if err != nil {
		t.Fatalf("decode response cursor: %v", err)
	}
	if next.LastPath != "/reports/c" || next.LastID != 12 {
		t.Fatalf("next cursor = %+v", next)
	}
}

func TestServiceCreateFileStreamsAndPersistsIntegrityMetadata(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	principal := serviceTestPrincipal()
	contents := []byte("hello world")
	cleanupJob := db.FilestoreObjectCleanupJob{ID: 93, ExternalID: "cleanup_create"}
	var enqueueInput db.EnqueueFilestoreObjectCleanupJobInput
	var putInput db.PutFilestoreFileInput
	var uploadKey string
	var uploadOptions storage.UploadOptions
	var uploadedBody []byte
	var attachedWorkspaceID int64
	var attachedJobExternalID string
	var attachedETag string
	var attachedVersionID string
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn:      serviceParentDirectoryLookup(filesystem),
		enqueueCleanupFn: func(_ context.Context, input db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			enqueueInput = input
			return cleanupJob, nil
		},
		putFileFn: func(_ context.Context, input db.PutFilestoreFileInput) (db.FilestoreMutationResult, error) {
			putInput = input
			return db.FilestoreMutationResult{Entry: serviceTestFileEntryFromBlob(filesystem, "file_created", input.Path, input.Blob)}, nil
		},
		attachCleanupFn: func(_ context.Context, workspaceID int64, jobExternalID, etag, versionID string) error {
			attachedWorkspaceID = workspaceID
			attachedJobExternalID = jobExternalID
			attachedETag = etag
			attachedVersionID = versionID
			return nil
		},
	}
	store := &fakeServiceBlobStore{
		uploadFn: func(_ context.Context, key string, body io.Reader, options storage.UploadOptions) (storage.UploadResult, error) {
			uploadKey = key
			uploadOptions = options
			var err error
			uploadedBody, err = io.ReadAll(body)
			return storage.UploadResult{Size: int64(len(uploadedBody)), ETag: "etag-create", VersionID: "version-create"}, err
		},
	}
	service := newServiceUnderTest(filestoreTestConfig(1024, 4096, "filestore-test"), database, store)
	response, apiErr := service.CreateFile(context.Background(), principal, createFileParams{
		FilesystemID: filesystem.ExternalID,
		Path:         "/report.txt",
		Metadata:     map[string]any{"source": "unit-test"},
		MediaType:    "Text/Plain",
		Authorization: &authorizationMetadata{
			Intent:       "assistant_output",
			Downloadable: true,
		},
		Tags:              []string{"report", "daily"},
		OverwriteExisting: true,
		TTLSeconds:        60,
	}, strings.NewReader(string(contents)))

	if apiErr != nil {
		t.Fatalf("CreateFile() error = %v", apiErr)
	}
	if string(uploadedBody) != string(contents) || uploadOptions.ContentType != "text/plain" {
		t.Fatalf("upload body/options = %q/%+v", uploadedBody, uploadOptions)
	}
	wantPrefix := "workspaces/" + principal.WorkspaceUUID + "/filestores/" + filesystem.UUID + "/blobs/"
	if !strings.HasPrefix(uploadKey, wantPrefix) {
		t.Fatalf("upload key = %q, want prefix %q", uploadKey, wantPrefix)
	}
	blobUUID := strings.TrimPrefix(uploadKey, wantPrefix)
	if _, err := uuid.Parse(blobUUID); err != nil {
		t.Fatalf("upload blob UUID = %q: %v", blobUUID, err)
	}
	if enqueueInput.Key != uploadKey || enqueueInput.EntryExternalID != blobUUID || enqueueInput.Bucket != "filestore-test" ||
		enqueueInput.Reason != "orphan_guard" || !enqueueInput.RunAfter.Equal(serviceTestNow.Add(orphanCleanupDelay)) {
		t.Fatalf("cleanup enqueue input = %+v", enqueueInput)
	}
	if attachedWorkspaceID != principal.WorkspaceID || attachedJobExternalID != cleanupJob.ExternalID ||
		attachedETag != "etag-create" || attachedVersionID != "version-create" {
		t.Fatalf("attached cleanup version = workspace %d, job %q, etag %q, version %q", attachedWorkspaceID, attachedJobExternalID, attachedETag, attachedVersionID)
	}
	md5Sum := md5.Sum(contents)
	sha256Sum := sha256.Sum256(contents)
	if putInput.Blob.SizeBytes != int64(len(contents)) || putInput.Blob.MD5 != hex.EncodeToString(md5Sum[:]) ||
		putInput.Blob.SHA256 != hex.EncodeToString(sha256Sum[:]) || putInput.Blob.S3Key != uploadKey ||
		putInput.Blob.S3Bucket != "filestore-test" || putInput.Blob.S3ETag != "etag-create" ||
		putInput.Blob.S3VersionID != "version-create" || putInput.Blob.MediaType != "text/plain" ||
		putInput.Blob.DetectedMimeType != "text/plain" || !putInput.Blob.Downloadable {
		t.Fatalf("put blob = %+v", putInput.Blob)
	}
	if putInput.Path != "/report.txt" || !putInput.OverwriteExisting || putInput.OrphanCleanupJobExternalID != cleanupJob.ExternalID ||
		putInput.WorkspaceStorageLimitBytes != 4096 || !putInput.Now.Equal(serviceTestNow) {
		t.Fatalf("put input = %+v", putInput)
	}
	if putInput.Blob.ExpiresAt == nil || !putInput.Blob.ExpiresAt.Equal(serviceTestNow.Add(time.Minute)) {
		t.Fatalf("expires at = %v", putInput.Blob.ExpiresAt)
	}
	var metadata map[string]any
	if err := json.Unmarshal(putInput.Blob.Metadata, &metadata); err != nil || metadata["source"] != "unit-test" {
		t.Fatalf("metadata = %s, error = %v", putInput.Blob.Metadata, err)
	}
	var authorization authorizationMetadata
	if err := json.Unmarshal(putInput.Blob.AuthorizationMetadata, &authorization); err != nil || authorization.Intent != "assistant_output" || !authorization.Downloadable {
		t.Fatalf("authorization metadata = %s, decoded = %+v, error = %v", putInput.Blob.AuthorizationMetadata, authorization, err)
	}
	if response.File.Path != "/report.txt" || response.File.FilesystemID != filesystem.ExternalID ||
		int64(response.File.File.Size) != int64(len(contents)) || response.File.File.MD5 != putInput.Blob.MD5 ||
		response.File.File.Metadata["source"] != "unit-test" {
		t.Fatalf("create response = %+v", response)
	}
}

func TestServiceCopyFilePreservesMetadataAndUsesCopiedObjectIdentity(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	principal := serviceTestPrincipal()
	source := serviceTestFileEntry(filesystem, "/source.txt", []byte("source bytes"))
	source.ExternalID = "file_source"
	source.Metadata = json.RawMessage("{\"owner\":\"agent\"}")
	source.AuthorizationMetadata = json.RawMessage("{\"intent\":\"assistant_output\",\"downloadable\":true}")
	source.Tags = []string{"source-tag"}
	source.Downloadable = true
	cleanupJob := db.FilestoreObjectCleanupJob{ID: 94, ExternalID: "cleanup_copy"}
	var enqueueInput db.EnqueueFilestoreObjectCleanupJobInput
	var copiedSourceKey string
	var copiedDestinationKey string
	var copyInput db.CopyFilestoreFileInput
	var attachedWorkspaceID int64
	var attachedJobExternalID string
	var attachedETag string
	var attachedVersionID string
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(_ context.Context, _ int64, _ int64, entryPath string) (db.FilestoreEntry, error) {
			switch entryPath {
			case "/source.txt":
				return source, nil
			case "/archive":
				return serviceTestDirectoryEntry(filesystem, 30, "/archive"), nil
			default:
				return db.FilestoreEntry{}, db.ErrNotFound
			}
		},
		enqueueCleanupFn: func(_ context.Context, input db.EnqueueFilestoreObjectCleanupJobInput) (db.FilestoreObjectCleanupJob, error) {
			enqueueInput = input
			return cleanupJob, nil
		},
		copyFileFn: func(_ context.Context, input db.CopyFilestoreFileInput) (db.FilestoreMutationResult, error) {
			copyInput = input
			blob := db.FilestoreFileBlob{
				SizeBytes:             *source.SizeBytes,
				MediaType:             stringValue(source.MediaType),
				DetectedMimeType:      stringValue(source.DetectedMimeType),
				Metadata:              source.Metadata,
				AuthorizationMetadata: source.AuthorizationMetadata,
				Tags:                  append([]string(nil), source.Tags...),
				Downloadable:          source.Downloadable,
				MD5:                   stringValue(source.MD5),
				SHA256:                stringValue(source.SHA256),
				S3Bucket:              input.DestinationS3Bucket,
				S3Key:                 input.DestinationS3Key,
				S3ETag:                input.DestinationS3ETag,
				S3VersionID:           input.DestinationS3VersionID,
			}
			return db.FilestoreMutationResult{Entry: serviceTestFileEntryFromBlob(filesystem, "file_copy", input.DestinationPath, blob)}, nil
		},
		attachCleanupFn: func(_ context.Context, workspaceID int64, jobExternalID, etag, versionID string) error {
			attachedWorkspaceID = workspaceID
			attachedJobExternalID = jobExternalID
			attachedETag = etag
			attachedVersionID = versionID
			return nil
		},
	}
	store := &fakeServiceBlobStore{
		copyFn: func(_ context.Context, sourceKey, destinationKey string) (storage.CopyResult, error) {
			copiedSourceKey = sourceKey
			copiedDestinationKey = destinationKey
			return storage.CopyResult{ETag: "etag-copy", VersionID: "version-copy"}, nil
		},
	}
	service := newServiceUnderTest(filestoreTestConfig(0, 2048, "filestore-test"), database, store)
	response, apiErr := service.CopyFile(context.Background(), principal, copyMoveFileRequest{
		FilesystemID:      filesystem.ExternalID,
		Source:            "/source.txt",
		Destination:       "/archive/copied.txt",
		OverwriteExisting: true,
	})

	if apiErr != nil {
		t.Fatalf("CopyFile() error = %v", apiErr)
	}
	if copiedSourceKey != stringValue(source.S3Key) || copiedDestinationKey == "" || copiedDestinationKey == copiedSourceKey {
		t.Fatalf("copy keys = source %q, destination %q", copiedSourceKey, copiedDestinationKey)
	}
	if enqueueInput.Key != copiedDestinationKey || enqueueInput.Bucket != "filestore-test" {
		t.Fatalf("copy cleanup input = %+v", enqueueInput)
	}
	assertCleanupEntryExternalIDMatchesBlobKey(t, enqueueInput)
	if attachedWorkspaceID != principal.WorkspaceID || attachedJobExternalID != cleanupJob.ExternalID ||
		attachedETag != "etag-copy" || attachedVersionID != "version-copy" {
		t.Fatalf("attached copy cleanup version = workspace %d, job %q, etag %q, version %q", attachedWorkspaceID, attachedJobExternalID, attachedETag, attachedVersionID)
	}
	if copyInput.SourcePath != "/source.txt" || copyInput.DestinationPath != "/archive/copied.txt" ||
		copyInput.ExpectedSourceS3Key != stringValue(source.S3Key) || copyInput.ExpectedSourceS3VersionID != stringValue(source.S3VersionID) ||
		copyInput.DestinationS3Key != copiedDestinationKey || copyInput.DestinationS3ETag != "etag-copy" ||
		copyInput.DestinationS3VersionID != "version-copy" || copyInput.OrphanCleanupJobExternalID != cleanupJob.ExternalID ||
		!copyInput.OverwriteExisting || copyInput.WorkspaceStorageLimitBytes != 2048 {
		t.Fatalf("copy database input = %+v", copyInput)
	}
	if response.File.Path != "/archive/copied.txt" || response.File.File.Metadata["owner"] != "agent" ||
		response.File.File.MD5 != stringValue(source.MD5) || !response.File.File.Downloadable ||
		len(response.File.File.Tags) != 1 || response.File.File.Tags[0] != "source-tag" {
		t.Fatalf("copy response = %+v", response)
	}
}

func TestServiceMoveOperationsReturnDatabaseEntries(t *testing.T) {
	t.Parallel()

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		filesystem := serviceTestFilesystem()
		var moveInput db.MoveFilestoreFileInput
		database := &fakeServiceDatabase{
			getFilesystemFn: serviceFilesystemLookup(filesystem),
			moveFileFn: func(_ context.Context, input db.MoveFilestoreFileInput) (db.FilestoreMutationResult, error) {
				moveInput = input
				return db.FilestoreMutationResult{Entry: serviceTestFileEntry(filesystem, input.DestinationPath, []byte("moved"))}, nil
			},
		}
		service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
		response, apiErr := service.MoveFile(context.Background(), serviceTestPrincipal(), copyMoveFileRequest{
			FilesystemID:      filesystem.ExternalID,
			Source:            "/old.txt",
			Destination:       "/new.txt",
			OverwriteExisting: true,
		})

		if apiErr != nil {
			t.Fatalf("MoveFile() error = %v", apiErr)
		}
		if moveInput.SourcePath != "/old.txt" || moveInput.DestinationPath != "/new.txt" ||
			!moveInput.OverwriteExisting || !moveInput.Now.Equal(serviceTestNow) {
			t.Fatalf("move file input = %+v", moveInput)
		}
		if response.File.Path != "/new.txt" || int64(response.File.File.Size) != 5 {
			t.Fatalf("move file response = %+v", response)
		}
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		filesystem := serviceTestFilesystem()
		var moveInput db.MoveFilestoreDirectoryInput
		database := &fakeServiceDatabase{
			getFilesystemFn: serviceFilesystemLookup(filesystem),
			moveDirectoryFn: func(_ context.Context, input db.MoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error) {
				moveInput = input
				return db.FilestoreMutationResult{Entry: serviceTestDirectoryEntry(filesystem, 40, input.DestinationPath)}, nil
			},
		}
		service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
		response, apiErr := service.MoveDirectory(context.Background(), serviceTestPrincipal(), moveDirectoryRequest{
			FilesystemID: filesystem.ExternalID,
			Source:       "/old",
			Destination:  "/new",
		})

		if apiErr != nil {
			t.Fatalf("MoveDirectory() error = %v", apiErr)
		}
		if moveInput.SourcePath != "/old" || moveInput.DestinationPath != "/new" || !moveInput.Now.Equal(serviceTestNow) {
			t.Fatalf("move directory input = %+v", moveInput)
		}
		if response.Directory.Path != "/new" || response.Directory.FilesystemID != filesystem.ExternalID {
			t.Fatalf("move directory response = %+v", response)
		}
	})
}

func TestServiceRemoveOperationsAreIdempotentForMissingEntries(t *testing.T) {
	t.Parallel()

	t.Run("file", func(t *testing.T) {
		t.Parallel()

		filesystem := serviceTestFilesystem()
		var removeInput db.RemoveFilestoreEntryInput
		database := &fakeServiceDatabase{
			getFilesystemFn: serviceFilesystemLookup(filesystem),
			removeFileFn: func(_ context.Context, input db.RemoveFilestoreEntryInput) (db.FilestoreMutationResult, error) {
				removeInput = input
				return db.FilestoreMutationResult{}, db.ErrNotFound
			},
		}
		service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
		apiErr := service.RemoveFile(context.Background(), serviceTestPrincipal(), pathRequest{FilesystemID: filesystem.ExternalID, Path: "/gone.txt"})

		if apiErr != nil {
			t.Fatalf("RemoveFile() error = %v", apiErr)
		}
		if removeInput.Path != "/gone.txt" || !removeInput.Now.Equal(serviceTestNow) {
			t.Fatalf("remove file input = %+v", removeInput)
		}
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()

		filesystem := serviceTestFilesystem()
		var removeInput db.RemoveFilestoreDirectoryInput
		database := &fakeServiceDatabase{
			getFilesystemFn: serviceFilesystemLookup(filesystem),
			removeDirectoryFn: func(_ context.Context, input db.RemoveFilestoreDirectoryInput) (db.FilestoreMutationResult, error) {
				removeInput = input
				return db.FilestoreMutationResult{}, db.ErrNotFound
			},
		}
		service := newServiceUnderTest(config.Config{}, database, &fakeServiceBlobStore{})
		apiErr := service.RemoveDirectory(context.Background(), serviceTestPrincipal(), removeDirectoryRequest{
			FilesystemID: filesystem.ExternalID,
			Path:         "/gone",
			Recursive:    true,
		})

		if apiErr != nil {
			t.Fatalf("RemoveDirectory() error = %v", apiErr)
		}
		if removeInput.Path != "/gone" || !removeInput.Recursive || !removeInput.Now.Equal(serviceTestNow) {
			t.Fatalf("remove directory input = %+v", removeInput)
		}
	})
}

func TestServiceReadFileClampsRangeAndReturnsObjectResponse(t *testing.T) {
	t.Parallel()

	filesystem := serviceTestFilesystem()
	entry := serviceTestFileEntry(filesystem, "/range.txt", []byte("0123456789"))
	var openedKey string
	var openedRange *storage.ByteRange
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(context.Context, int64, int64, string) (db.FilestoreEntry, error) {
			return entry, nil
		},
	}
	store := &fakeServiceBlobStore{
		openFn: func(_ context.Context, key string, byteRange *storage.ByteRange) (storage.Object, error) {
			openedKey = key
			if byteRange != nil {
				copyRange := *byteRange
				openedRange = &copyRange
			}
			return storage.Object{Body: io.NopCloser(strings.NewReader("23456789")), Size: 8, ContentType: "ignored/type"}, nil
		},
	}
	service := newServiceUnderTest(config.Config{}, database, store)
	result, apiErr := service.ReadFile(context.Background(), serviceTestPrincipal(), readFileRequest{
		FilesystemID: filesystem.ExternalID,
		Path:         "/range.txt",
		Range:        &readFileRange{Offset: 2, Length: 99},
	})

	if apiErr != nil {
		t.Fatalf("ReadFile() error = %v", apiErr)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if openedKey != stringValue(entry.S3Key) || openedRange == nil || openedRange.Offset != 2 || openedRange.Length != 8 {
		t.Fatalf("open key/range = %q/%+v", openedKey, openedRange)
	}
	if result.Size != 8 || result.MediaType != "text/plain" || string(data) != "23456789" {
		t.Fatalf("read result = size %d, media type %q, body %q", result.Size, result.MediaType, data)
	}
}

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
