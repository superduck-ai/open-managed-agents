package filestore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

func TestSkillArchiveViewRejectsInvalidArchives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		files      map[string]string
		mutateHash bool
	}{
		{
			name:       "checksum mismatch",
			files:      map[string]string{"demo/SKILL.md": "# Demo"},
			mutateHash: true,
		},
		{
			name:  "missing SKILL md",
			files: map[string]string{"demo/README.md": "missing"},
		},
		{
			name:  "path traversal",
			files: map[string]string{"demo/SKILL.md": "# Demo", "demo/../secret": "no"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			archiveBytes := buildSkillArchiveTestZip(t, test.files)
			archiveEntry := skillArchiveTestEntry(archiveBytes)
			if test.mutateHash {
				archiveEntry.SHA256 = serviceTestPointer(string(bytes.Repeat([]byte{'0'}, 64)))
			}
			service, _ := skillArchiveTestService(archiveBytes, archiveEntry)
			_, apiErr := service.ListDirectory(context.Background(), serviceTestPrincipal(), listDirectoryRequest{
				FilesystemID: "fs_test",
				Path:         archiveEntry.Path,
			})
			assertServiceAPIError(t, apiErr, http.StatusInternalServerError, "internal")
		})
	}
}

func TestSkillArchiveLoadRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	archiveBytes := buildSkillArchiveTestZip(t, map[string]string{"demo/SKILL.md": "# Demo"})
	archiveEntry := skillArchiveTestEntry(archiveBytes)
	var openCount atomic.Int32
	backend := &skillArchivePathBackend{
		store: &fakeServiceBlobStore{
			openFn: func(context.Context, string, *storage.ByteRange) (storage.Object, error) {
				if openCount.Add(1) == 1 {
					return storage.Object{}, storage.ErrNotFound
				}
				return storage.Object{
					Body: io.NopCloser(bytes.NewReader(archiveBytes)),
					Size: int64(len(archiveBytes)),
				}, nil
			},
		},
		cache: newSkillArchiveCache(defaultSkillArchiveCacheEntries),
	}

	_, apiErr := backend.loadSkillArchive(context.Background(), archiveEntry)
	assertServiceAPIError(t, apiErr, http.StatusNotFound, "not_found")

	archive, apiErr := backend.loadSkillArchive(context.Background(), archiveEntry)
	if apiErr != nil {
		t.Fatalf("loadSkillArchive() retry error = %v", apiErr)
	}
	if _, ok := archive.nodes["/skills/demo/SKILL.md"]; !ok {
		t.Fatalf("loadSkillArchive() retry archive = %#v, want SKILL.md", archive)
	}
	if got := openCount.Load(); got != 2 {
		t.Fatalf("object opens = %d, want 2", got)
	}
}

func TestSkillArchiveCacheKeepsTwentyMostRecentArchives(t *testing.T) {
	t.Parallel()

	cache := newSkillArchiveCache(defaultSkillArchiveCacheEntries)
	for index := range defaultSkillArchiveCacheEntries {
		cache.Add(
			fmt.Sprintf("skill-%d", index),
			&loadedSkillArchive{data: []byte{byte(index)}},
		)
	}
	if _, ok := cache.Get("skill-0"); !ok {
		t.Fatal("cache does not contain the oldest inserted archive")
	}

	cache.Add("skill-20", &loadedSkillArchive{data: []byte{20}})

	if _, ok := cache.Peek("skill-1"); ok {
		t.Fatal("cache retained the least recently used archive after exceeding 20 entries")
	}
	if _, ok := cache.Peek("skill-0"); !ok {
		t.Fatal("cache evicted an archive refreshed before capacity was exceeded")
	}
	if got := cache.Len(); got != defaultSkillArchiveCacheEntries {
		t.Fatalf("cache length = %d, want %d", got, defaultSkillArchiveCacheEntries)
	}
}

func TestSkillArchiveNamespaceIsReadOnly(t *testing.T) {
	t.Parallel()

	service := newServiceUnderTest(
		filestoreTestConfig(1024, 4096, "filestore-test"),
		&fakeServiceDatabase{getFilesystemFn: serviceFilesystemLookup(serviceTestFilesystem())},
		&fakeServiceBlobStore{},
	)
	principal := serviceTestPrincipal()
	tests := []struct {
		name string
		run  func() *apiError
	}{
		{
			name: "make directory",
			run: func() *apiError {
				_, apiErr := service.MakeDirectory(context.Background(), principal, makeDirectoryRequest{
					FilesystemID: "fs_test",
					Path:         "/skills/demo/new",
				})
				return apiErr
			},
		},
		{
			name: "create file",
			run: func() *apiError {
				_, apiErr := service.CreateFile(context.Background(), principal, createFileParams{
					FilesystemID: "fs_test",
					Path:         "/skills/demo/new.txt",
					MediaType:    "text/plain",
				}, bytes.NewReader(nil))
				return apiErr
			},
		},
		{
			name: "remove directory",
			run: func() *apiError {
				return service.RemoveDirectory(context.Background(), principal, removeDirectoryRequest{
					FilesystemID: "fs_test",
					Path:         "/skills/demo",
					Recursive:    true,
				})
			},
		},
		{
			name: "remove file",
			run: func() *apiError {
				return service.RemoveFile(context.Background(), principal, pathRequest{
					FilesystemID: "fs_test",
					Path:         "/skills/demo/a.txt",
				})
			},
		},
		{
			name: "copy from skills",
			run: func() *apiError {
				_, apiErr := service.CopyFile(context.Background(), principal, copyMoveFileRequest{
					FilesystemID: "fs_test",
					Source:       "/skills/demo/a.txt",
					Destination:  "/outputs/a.txt",
				})
				return apiErr
			},
		},
		{
			name: "copy into skills",
			run: func() *apiError {
				_, apiErr := service.CopyFile(context.Background(), principal, copyMoveFileRequest{
					FilesystemID: "fs_test",
					Source:       "/outputs/a.txt",
					Destination:  "/skills/demo/a.txt",
				})
				return apiErr
			},
		},
		{
			name: "move file from skills",
			run: func() *apiError {
				_, apiErr := service.MoveFile(context.Background(), principal, copyMoveFileRequest{
					FilesystemID: "fs_test",
					Source:       "/skills/demo/a.txt",
					Destination:  "/outputs/a.txt",
				})
				return apiErr
			},
		},
		{
			name: "move file into skills",
			run: func() *apiError {
				_, apiErr := service.MoveFile(context.Background(), principal, copyMoveFileRequest{
					FilesystemID: "fs_test",
					Source:       "/outputs/a.txt",
					Destination:  "/skills/demo/a.txt",
				})
				return apiErr
			},
		},
		{
			name: "move directory from skills",
			run: func() *apiError {
				_, apiErr := service.MoveDirectory(context.Background(), principal, moveDirectoryRequest{
					FilesystemID: "fs_test",
					Source:       "/skills/demo",
					Destination:  "/outputs/demo",
				})
				return apiErr
			},
		},
		{
			name: "move directory into skills",
			run: func() *apiError {
				_, apiErr := service.MoveDirectory(context.Background(), principal, moveDirectoryRequest{
					FilesystemID: "fs_test",
					Source:       "/outputs/demo",
					Destination:  "/skills/demo",
				})
				return apiErr
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertServiceAPIError(t, test.run(), http.StatusForbidden, "permission_denied")
		})
	}
}

func TestSkillArchiveConcurrentColdLoadUsesSingleObjectRead(t *testing.T) {
	t.Parallel()

	archiveBytes := buildSkillArchiveTestZip(t, map[string]string{"demo/SKILL.md": "# Demo"})
	archiveEntry := skillArchiveTestEntry(archiveBytes)
	firstOpen := make(chan struct{})
	secondOpen := make(chan struct{})
	releaseOpen := make(chan struct{})
	var openCount atomic.Int32
	backend := &skillArchivePathBackend{
		store: &fakeServiceBlobStore{
			openFn: func(context.Context, string, *storage.ByteRange) (storage.Object, error) {
				switch openCount.Add(1) {
				case 1:
					close(firstOpen)
				case 2:
					close(secondOpen)
				}
				<-releaseOpen
				return storage.Object{
					Body: io.NopCloser(bytes.NewReader(archiveBytes)),
					Size: int64(len(archiveBytes)),
				}, nil
			},
		},
		cache: newSkillArchiveCache(defaultSkillArchiveCacheEntries),
	}

	const callers = 16
	start := make(chan struct{})
	archives := make([]*loadedSkillArchive, callers)
	apiErrors := make([]*apiError, callers)
	var ready sync.WaitGroup
	var complete sync.WaitGroup
	ready.Add(callers)
	complete.Add(callers)
	for index := range callers {
		go func() {
			defer complete.Done()
			ready.Done()
			<-start
			archives[index], apiErrors[index] = backend.loadSkillArchive(context.Background(), archiveEntry)
		}()
	}
	ready.Wait()
	close(start)
	<-firstOpen

	duplicateOpen := false
	select {
	case <-secondOpen:
		duplicateOpen = true
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseOpen)
	complete.Wait()

	if duplicateOpen {
		t.Fatalf("concurrent cold load opened the archive %d times, want 1", openCount.Load())
	}
	if got := openCount.Load(); got != 1 {
		t.Fatalf("object opens = %d, want 1", got)
	}
	for index := range callers {
		if apiErrors[index] != nil {
			t.Fatalf("loadSkillArchive() caller %d error = %v", index, apiErrors[index])
		}
		if archives[index] != archives[0] {
			t.Fatalf("loadSkillArchive() caller %d received a different archive pointer", index)
		}
	}
}

func TestSkillArchiveViewListsMetadataAndReadsRanges(t *testing.T) {
	t.Parallel()

	archiveBytes := buildSkillArchiveTestZip(t, map[string]string{
		"demo/SKILL.md":      "# Demo",
		"demo/docs/guide.md": "0123456789",
	})
	archiveEntry := skillArchiveTestEntry(archiveBytes)
	service, openCount := skillArchiveTestService(archiveBytes, archiveEntry)
	ctx := context.Background()
	principal := serviceTestPrincipal()

	root, apiErr := service.ListDirectory(ctx, principal, listDirectoryRequest{
		FilesystemID: "fs_test",
		Path:         "/skills",
	})
	if apiErr != nil {
		t.Fatalf("ListDirectory(/skills) error = %v", apiErr)
	}
	if len(root.Entries) != 1 || root.Entries[0].Directory == nil || root.Entries[0].Directory.Path != "/skills/demo" {
		t.Fatalf("ListDirectory(/skills) = %#v", root)
	}
	if *openCount != 0 {
		t.Fatalf("top-level ListDirectory(/skills) object opens = %d, want 0", *openCount)
	}

	rootMetadata, apiErr := service.ReadMetadata(ctx, principal, pathRequest{
		FilesystemID: "fs_test",
		Path:         "/skills",
	})
	if apiErr != nil {
		t.Fatalf("ReadMetadata(/skills) error = %v", apiErr)
	}
	if rootMetadata.Directory == nil || rootMetadata.Directory.Path != "/skills" {
		t.Fatalf("ReadMetadata(/skills) = %#v", rootMetadata)
	}

	nested, apiErr := service.ListDirectory(ctx, principal, listDirectoryRequest{
		FilesystemID: "fs_test",
		Path:         "/skills/demo",
		Recursive:    true,
	})
	if apiErr != nil {
		t.Fatalf("ListDirectory(/skills/demo) error = %v", apiErr)
	}
	if len(nested.Entries) != 3 {
		t.Fatalf("recursive entry count = %d, want 3: %#v", len(nested.Entries), nested)
	}

	metadata, apiErr := service.ReadMetadata(ctx, principal, pathRequest{
		FilesystemID: "fs_test",
		Path:         "/skills/demo/docs/guide.md",
	})
	if apiErr != nil {
		t.Fatalf("ReadMetadata() error = %v", apiErr)
	}
	if metadata.File == nil || int64(metadata.File.File.Size) != 10 || metadata.File.Path != "/skills/demo/docs/guide.md" {
		t.Fatalf("ReadMetadata() = %#v", metadata)
	}

	read, apiErr := service.ReadFile(ctx, principal, readFileRequest{
		FilesystemID: "fs_test",
		Path:         "/skills/demo/docs/guide.md",
		Range:        &readFileRange{Offset: 3, Length: 4},
	})
	if apiErr != nil {
		t.Fatalf("ReadFile() error = %v", apiErr)
	}
	body, err := io.ReadAll(read.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := read.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if string(body) != "3456" || read.Size != 4 {
		t.Fatalf("ReadFile() body/size = %q/%d", body, read.Size)
	}
	if *openCount != 1 {
		t.Fatalf("object opens = %d, want one cached load", *openCount)
	}
}

func skillArchiveTestService(
	archiveBytes []byte,
	archiveEntry db.FilestoreEntry,
) (*Service, *int) {
	filesystem := serviceTestFilesystem()
	openCount := 0
	database := &fakeServiceDatabase{
		getFilesystemFn: serviceFilesystemLookup(filesystem),
		getEntryFn: func(_ context.Context, workspaceID, filesystemID int64, entryPath string) (db.FilestoreEntry, error) {
			if workspaceID != serviceTestPrincipal().WorkspaceID ||
				filesystemID != filesystem.ID ||
				entryPath != skillNamespacePath {
				return db.FilestoreEntry{}, db.ErrNotFound
			}
			return serviceTestDirectoryEntry(filesystem, 70, skillNamespacePath), nil
		},
		listSkillArchiveEntriesFn: func(context.Context, int64, int64) ([]db.FilestoreEntry, error) {
			return []db.FilestoreEntry{archiveEntry}, nil
		},
	}
	store := &fakeServiceBlobStore{
		openFn: func(_ context.Context, key string, byteRange *storage.ByteRange) (storage.Object, error) {
			openCount++
			if key != *archiveEntry.S3Key || byteRange != nil {
				t := storage.ErrNotFound
				return storage.Object{}, t
			}
			return storage.Object{
				Body: io.NopCloser(bytes.NewReader(archiveBytes)),
				Size: int64(len(archiveBytes)),
			}, nil
		},
	}
	return newServiceUnderTest(filestoreTestConfig(1024, 4096, "filestore-test"), database, store), &openCount
}

func skillArchiveTestEntry(data []byte) db.FilestoreEntry {
	sum := sha256.Sum256(data)
	return db.FilestoreEntry{
		ID:                  71,
		UUID:                "77777777-7777-4777-8777-777777777777",
		ExternalID:          "fse_test",
		OrganizationUUID:    serviceTestPrincipal().OrganizationUUID,
		WorkspaceUUID:       serviceTestPrincipal().WorkspaceUUID,
		FilesystemUUID:      serviceTestFilesystem().UUID,
		Kind:                db.FilestoreEntryKindArchive,
		Path:                "/skills/demo",
		ParentPath:          serviceTestPointer("/skills"),
		SizeBytes:           serviceTestPointer(int64(len(data))),
		MediaType:           serviceTestPointer("application/zip"),
		DetectedMimeType:    serviceTestPointer("application/zip"),
		SHA256:              serviceTestPointer(hex.EncodeToString(sum[:])),
		S3Bucket:            serviceTestPointer("filestore-test"),
		S3Key:               serviceTestPointer("skills/demo/1.zip"),
		ManagedBy:           serviceTestPointer("skill_archive"),
		ManagedResourceUUID: serviceTestPointer("88888888-8888-4888-8888-888888888888"),
		CreatedAt:           serviceTestNow,
		UpdatedAt:           serviceTestNow,
	}
}

func buildSkillArchiveTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := io.WriteString(entry, contents); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}
