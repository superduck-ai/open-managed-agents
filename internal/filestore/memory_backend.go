package filestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/memorypath"
	"github.com/superduck-ai/open-managed-agents/internal/sessionresource"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
)

type memoryFilestoreStore interface {
	ListSessionMemoryMounts(ctx context.Context, workspaceUUID, filesystemUUID string) ([]db.SessionMemoryMount, error)
	GetMemoryByPath(ctx context.Context, workspaceUUID, memoryStoreExternalID, path string) (db.Memory, bool, error)
	ListMemoriesPage(ctx context.Context, params db.ListMemoriesPageParams) ([]db.Memory, bool, error)
	ListMemoriesForDepth(ctx context.Context, params db.ListMemoriesPageParams) ([]db.Memory, error)
	CreateMemory(ctx context.Context, memory db.Memory, version db.MemoryVersion) (db.Memory, error)
	UpdateMemory(ctx context.Context, input db.UpdateMemoryInput) (db.MemoryMutationResult, error)
	MoveMemory(ctx context.Context, input db.MoveMemoryInput) (db.MemoryMutationResult, error)
	DeleteMemory(ctx context.Context, input db.DeleteMemoryInput) error
}

type memoryPathBackend struct {
	memories memoryFilestoreStore
	store    storage.ObjectStore
	now      func() time.Time
}

func (b *memoryPathBackend) createFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	params createFileParams,
	body io.Reader,
) (fileResponse, *apiError) {
	parsed, _ := parseMemoryFilestorePath(params.Path)
	if apiErr := requireMemoryDocumentPath(parsed); apiErr != nil {
		return fileResponse{}, apiErr
	}
	if params.TTLSeconds != 0 {
		return fileResponse{}, invalidArgument("ttlSeconds is not supported for memory files")
	}
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, true)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	content, apiErr := readMemoryUpload(body)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	record, apiErr := b.upsertMemoryContent(ctx, principal, mount, parsed.Rel, content)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	return fileResponse{File: memoryFilePayload(record, filesystem.ExternalID, parsed.filestorePath(), params.MediaType)}, nil
}

func (b *memoryPathBackend) copyFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request copyMoveFileRequest,
) (fileResponse, *apiError) {
	source, _ := parseMemoryFilestorePath(request.Source)
	dest, _ := parseMemoryFilestorePath(request.Destination)
	if apiErr := requireMemoryDocumentPath(source); apiErr != nil {
		return fileResponse{}, apiErr
	}
	if apiErr := requireMemoryDocumentPath(dest); apiErr != nil {
		return fileResponse{}, apiErr
	}
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, source, true)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	current, apiErr := b.loadMemoryFile(ctx, principal.WorkspaceUUID, mount, source.Rel)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	content, apiErr := b.readMemoryObject(ctx, current)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	record, apiErr := b.upsertMemoryContent(ctx, principal, mount, dest.Rel, content)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	return fileResponse{File: memoryFilePayload(record, filesystem.ExternalID, dest.filestorePath(), "text/plain")}, nil
}

func (b *memoryPathBackend) moveFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request copyMoveFileRequest,
) (fileResponse, *apiError) {
	source, _ := parseMemoryFilestorePath(request.Source)
	dest, _ := parseMemoryFilestorePath(request.Destination)
	if apiErr := requireMemoryDocumentPath(source); apiErr != nil {
		return fileResponse{}, apiErr
	}
	if apiErr := requireMemoryDocumentPath(dest); apiErr != nil {
		return fileResponse{}, apiErr
	}
	if source.Rel == dest.Rel {
		current, apiErr := b.loadMountedMemoryFile(ctx, principal, filesystem, source, false)
		if apiErr != nil {
			return fileResponse{}, apiErr
		}
		return fileResponse{File: memoryFilePayload(current, filesystem.ExternalID, dest.filestorePath(), "text/plain")}, nil
	}
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, source, true)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	current, apiErr := b.loadMemoryFile(ctx, principal.WorkspaceUUID, mount, source.Rel)
	if apiErr != nil {
		return fileResponse{}, apiErr
	}
	deleteVersionID, err := ids.New("memver_")
	if err != nil {
		return fileResponse{}, internalError("allocate memory version id", err)
	}
	sourceVersionID, err := ids.New("memver_")
	if err != nil {
		return fileResponse{}, internalError("allocate memory version id", err)
	}
	destPath := dest.Rel
	result, err := b.memories.MoveMemory(ctx, db.MoveMemoryInput{
		WorkspaceUUID:                      principal.WorkspaceUUID,
		MemoryStoreExternalID:              mount.MemoryStoreExternalID,
		SourceMemoryExternalID:             current.ExternalID,
		DestinationPath:                    destPath,
		SourceVersionUUID:                  uuid.NewV4().String(),
		SourceVersionExternalID:            sourceVersionID,
		DestinationDeleteVersionUUID:       uuid.NewV4().String(),
		DestinationDeleteVersionExternalID: deleteVersionID,
		Actor:                              sessionMemoryActor(mount.SessionExternalID),
		Now:                                b.now().UTC(),
	})
	if err != nil {
		return fileResponse{}, mapMemoryMutationError("move memory", err)
	}
	return fileResponse{File: memoryFilePayload(result.Memory, filesystem.ExternalID, dest.filestorePath(), "text/plain")}, nil
}

func (b *memoryPathBackend) makeDirectory(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request makeDirectoryRequest,
) (directoryResponse, *apiError) {
	parsed, _ := parseMemoryFilestorePath(request.Path)
	if _, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, true); apiErr != nil {
		return directoryResponse{}, apiErr
	}
	return directoryResponse{Directory: virtualMemoryDirectory(filesystem.ExternalID, request.Path, b.now().UTC())}, nil
}

func (b *memoryPathBackend) removeDirectory(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request removeDirectoryRequest,
) *apiError {
	parsed, _ := parseMemoryFilestorePath(request.Path)
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, true)
	if apiErr != nil {
		return apiErr
	}
	if parsed.Rel != "" {
		if _, found, err := b.memories.GetMemoryByPath(ctx, principal.WorkspaceUUID, mount.MemoryStoreExternalID, parsed.Rel); err != nil {
			return mapMemoryMutationError("remove memory directory", err)
		} else if found {
			return failedPrecondition("path is not a directory")
		}
	}
	occupied, err := b.memoryDirectoryHasChildren(ctx, principal.WorkspaceUUID, mount.MemoryStoreExternalID, parsed.Rel)
	if err != nil {
		return mapMemoryMutationError("remove memory directory", err)
	}
	if occupied {
		return failedPrecondition("directory is not empty")
	}
	return nil
}

func (b *memoryPathBackend) removeFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request pathRequest,
) *apiError {
	parsed, _ := parseMemoryFilestorePath(request.Path)
	if apiErr := requireMemoryDocumentPath(parsed); apiErr != nil {
		return apiErr
	}
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, true)
	if apiErr != nil {
		return apiErr
	}
	current, found, err := b.memories.GetMemoryByPath(ctx, principal.WorkspaceUUID, mount.MemoryStoreExternalID, parsed.Rel)
	if err != nil {
		return mapMemoryMutationError("remove memory", err)
	}
	if !found {
		return nil
	}
	return b.deleteMemoryRecord(ctx, principal.WorkspaceUUID, mount, current)
}

func (b *memoryPathBackend) listDirectory(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request listDirectoryRequest,
	cursor directoryCursor,
	limit int,
) (listDirectoryResponse, *apiError) {
	parsed, _ := parseMemoryFilestorePath(request.Path)
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, false)
	if apiErr != nil {
		return listDirectoryResponse{}, apiErr
	}
	if parsed.Rel != "" {
		if _, found, err := b.memories.GetMemoryByPath(ctx, principal.WorkspaceUUID, mount.MemoryStoreExternalID, parsed.Rel); err != nil {
			return listDirectoryResponse{}, mapMemoryMutationError("list memory directory", err)
		} else if found {
			return listDirectoryResponse{}, failedPrecondition("path is not a directory")
		}
	}
	records, err := b.memories.ListMemoriesForDepth(ctx, db.ListMemoriesPageParams{
		WorkspaceUUID:         principal.WorkspaceUUID,
		MemoryStoreExternalID: mount.MemoryStoreExternalID,
		PathPrefix:            memoryListPrefix(parsed.Rel),
	})
	if err != nil {
		return listDirectoryResponse{}, mapMemoryMutationError("list memory directory", err)
	}
	entries := collectMemoryDirectoryEntries(parsed, request.Recursive, records)
	return paginateMemoryDirectory(request, cursor, limit, entries, filesystem.ExternalID)
}

func (b *memoryPathBackend) readFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	request readFileRequest,
) (readFileResult, *apiError) {
	parsed, _ := parseMemoryFilestorePath(request.Path)
	if apiErr := requireMemoryDocumentPath(parsed); apiErr != nil {
		return readFileResult{}, apiErr
	}
	current, apiErr := b.loadMountedMemoryFile(ctx, principal, filesystem, parsed, false)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	objectRange, responseSize, apiErr := resolveReadRange(request.Range, current.ContentSizeBytes)
	if apiErr != nil {
		return readFileResult{}, apiErr
	}
	if responseSize == 0 {
		return readFileResult{Body: io.NopCloser(bytes.NewReader(nil)), MediaType: "text/plain"}, nil
	}
	object, err := b.store.Open(ctx, current.S3Key, objectRange)
	if err != nil {
		return readFileResult{}, mapBlobstoreError("read memory", err)
	}
	return readFileResult{Body: object.Body, Size: responseSize, MediaType: "text/plain"}, nil
}

func (b *memoryPathBackend) readMetadata(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	entryPath string,
) (entryPayload, *apiError) {
	parsed, _ := parseMemoryFilestorePath(entryPath)
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, false)
	if apiErr != nil {
		return entryPayload{}, apiErr
	}
	if parsed.Rel != "" {
		current, found, err := b.memories.GetMemoryByPath(ctx, principal.WorkspaceUUID, mount.MemoryStoreExternalID, parsed.Rel)
		if err != nil {
			return entryPayload{}, mapMemoryMutationError("read memory metadata", err)
		}
		if found {
			file := memoryFilePayload(current, filesystem.ExternalID, parsed.filestorePath(), "text/plain")
			return entryPayload{File: &file}, nil
		}
	}
	directory := virtualMemoryDirectory(filesystem.ExternalID, entryPath, b.now().UTC())
	return entryPayload{Directory: &directory}, nil
}

func (b *memoryPathBackend) upsertMemoryContent(
	ctx context.Context,
	principal Principal,
	mount db.SessionMemoryMount,
	documentPath string,
	content []byte,
) (db.Memory, *apiError) {
	existing, found, err := b.memories.GetMemoryByPath(ctx, principal.WorkspaceUUID, mount.MemoryStoreExternalID, documentPath)
	if err != nil {
		return db.Memory{}, mapMemoryMutationError("lookup memory", err)
	}
	memoryUUID := uuid.NewV4().String()
	memoryID, err := ids.New("mem_")
	if err != nil {
		return db.Memory{}, internalError("allocate memory id", err)
	}
	if found {
		memoryUUID = existing.UUID
		memoryID = existing.ExternalID
	}
	versionID, err := ids.New("memver_")
	if err != nil {
		return db.Memory{}, internalError("allocate memory version id", err)
	}
	versionUUID := uuid.NewV4().String()
	objectKey := db.MemoryContentObjectKey(principal.WorkspaceUUID, mount.MemoryStoreUUID, memoryUUID, versionUUID)
	contentSHA := sha256HexBytes(content)
	if _, err := b.store.Upload(ctx, objectKey, bytes.NewReader(content), storage.UploadOptions{
		Size:        int64(len(content)),
		ContentType: "text/plain; charset=utf-8",
	}); err != nil {
		return db.Memory{}, mapBlobstoreError("upload memory", err)
	}
	now := b.now().UTC()
	actor := sessionMemoryActor(mount.SessionExternalID)
	if found {
		result, err := b.memories.UpdateMemory(ctx, db.UpdateMemoryInput{
			WorkspaceUUID:         principal.WorkspaceUUID,
			MemoryStoreExternalID: mount.MemoryStoreExternalID,
			MemoryExternalID:      existing.ExternalID,
			VersionUUID:           versionUUID,
			VersionExternalID:     versionID,
			ContentProvided:       true,
			ContentSizeBytes:      int64(len(content)),
			ContentSHA256:         contentSHA,
			S3Bucket:              b.store.Name(),
			S3Key:                 objectKey,
			Actor:                 actor,
			Now:                   now,
		})
		if err != nil {
			b.discardMemoryObject(ctx, objectKey)
			return db.Memory{}, mapMemoryMutationError("update memory", err)
		}
		if !result.VersionCreated {
			b.discardMemoryObject(ctx, objectKey)
		}
		return result.Memory, nil
	}
	pathValue := documentPath
	record, err := b.memories.CreateMemory(ctx, db.Memory{
		UUID:                  memoryUUID,
		ExternalID:            memoryID,
		OrganizationUUID:      principal.OrganizationUUID,
		WorkspaceUUID:         principal.WorkspaceUUID,
		MemoryStoreExternalID: mount.MemoryStoreExternalID,
		Path:                  documentPath,
		ContentSizeBytes:      int64(len(content)),
		ContentSHA256:         contentSHA,
		S3Bucket:              b.store.Name(),
		S3Key:                 objectKey,
		CreatedAt:             now,
		UpdatedAt:             now,
	}, db.MemoryVersion{
		UUID:             versionUUID,
		ExternalID:       versionID,
		Operation:        "created",
		Path:             &pathValue,
		ContentSizeBytes: ptrInt64(int64(len(content))),
		ContentSHA256:    &contentSHA,
		S3Bucket:         ptrString(b.store.Name()),
		S3Key:            &objectKey,
		CreatedBy:        actor,
		CreatedAt:        now,
	})
	if err != nil {
		b.discardMemoryObject(ctx, objectKey)
		return db.Memory{}, mapMemoryMutationError("create memory", err)
	}
	return record, nil
}

func (b *memoryPathBackend) deleteMemoryRecord(ctx context.Context, workspaceUUID string, mount db.SessionMemoryMount, current db.Memory) *apiError {
	versionID, err := ids.New("memver_")
	if err != nil {
		return internalError("allocate memory version id", err)
	}
	err = b.memories.DeleteMemory(ctx, db.DeleteMemoryInput{
		WorkspaceUUID:         workspaceUUID,
		MemoryStoreExternalID: mount.MemoryStoreExternalID,
		MemoryExternalID:      current.ExternalID,
		VersionUUID:           uuid.NewV4().String(),
		VersionExternalID:     versionID,
		Actor:                 sessionMemoryActor(mount.SessionExternalID),
		Now:                   b.now().UTC(),
	})
	if err != nil {
		return mapMemoryMutationError("delete memory", err)
	}
	return nil
}

func (b *memoryPathBackend) loadMountedMemoryFile(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	parsed memoryFilestorePath,
	mutate bool,
) (db.Memory, *apiError) {
	mount, apiErr := b.resolveMount(ctx, principal, filesystem, parsed, mutate)
	if apiErr != nil {
		return db.Memory{}, apiErr
	}
	return b.loadMemoryFile(ctx, principal.WorkspaceUUID, mount, parsed.Rel)
}

func (b *memoryPathBackend) loadMemoryFile(ctx context.Context, workspaceUUID string, mount db.SessionMemoryMount, documentPath string) (db.Memory, *apiError) {
	current, found, err := b.memories.GetMemoryByPath(ctx, workspaceUUID, mount.MemoryStoreExternalID, documentPath)
	if err != nil {
		return db.Memory{}, mapMemoryMutationError("read memory", err)
	}
	if !found {
		return db.Memory{}, notFound("resource does not exist")
	}
	return current, nil
}

func (b *memoryPathBackend) readMemoryObject(ctx context.Context, current db.Memory) ([]byte, *apiError) {
	object, err := b.store.Open(ctx, current.S3Key, nil)
	if err != nil {
		return nil, mapBlobstoreError("read memory", err)
	}
	defer object.Body.Close()
	return readMemoryUpload(object.Body)
}

func (b *memoryPathBackend) memoryDirectoryHasChildren(ctx context.Context, workspaceUUID, storeID, dirRel string) (bool, error) {
	records, _, err := b.memories.ListMemoriesPage(ctx, db.ListMemoriesPageParams{
		WorkspaceUUID:         workspaceUUID,
		MemoryStoreExternalID: storeID,
		Limit:                 1,
		PathPrefix:            memoryListPrefix(dirRel),
	})
	return len(records) > 0, err
}

func (b *memoryPathBackend) resolveMount(
	ctx context.Context,
	principal Principal,
	filesystem db.FilestoreFilesystem,
	parsed memoryFilestorePath,
	mutate bool,
) (db.SessionMemoryMount, *apiError) {
	if parsed.Rel != "" {
		if err := memorypath.Validate(parsed.Rel); err != nil {
			return db.SessionMemoryMount{}, invalidArgument(err.Error())
		}
	}
	mounts, err := b.memories.ListSessionMemoryMounts(ctx, principal.WorkspaceUUID, filesystem.UUID)
	if err != nil {
		return db.SessionMemoryMount{}, internalError("list memory mounts", err)
	}
	var mount db.SessionMemoryMount
	found := false
	for _, candidate := range mounts {
		if sessionresource.MemorySlugFromMountPath(candidate.MountPath) != parsed.Slug {
			continue
		}
		if found {
			return db.SessionMemoryMount{}, duplicateMemoryMountError()
		}
		mount, found = candidate, true
	}
	if !found || mount.StoreMissing || mount.MemoryStoreUUID == "" {
		return db.SessionMemoryMount{}, notFound("resource does not exist")
	}
	if mutate && mount.Archived {
		return db.SessionMemoryMount{}, permissionDenied("memory store is archived")
	}
	if mutate && mount.Access != string(sessionresource.MemoryAccessReadWrite) {
		return db.SessionMemoryMount{}, permissionDenied("the memory mount is read-only")
	}
	return mount, nil
}

func (b *memoryPathBackend) discardMemoryObject(ctx context.Context, key string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	_ = b.store.Delete(cleanupCtx, key, storage.DeleteOptions{})
}

func requireMemoryDocumentPath(parsed memoryFilestorePath) *apiError {
	if parsed.Rel == "" {
		return invalidArgument("path is a directory")
	}
	if err := memorypath.Validate(parsed.Rel); err != nil {
		return invalidArgument(err.Error())
	}
	return nil
}

func readMemoryUpload(body io.Reader) ([]byte, *apiError) {
	if body == nil {
		return nil, invalidArgument("file body is required")
	}
	data, err := io.ReadAll(io.LimitReader(body, int64(db.MaxMemoryContentBytes)+1))
	if err != nil {
		return nil, internalError("read memory body", err)
	}
	if len(data) > db.MaxMemoryContentBytes {
		return nil, &apiError{Status: http.StatusRequestEntityTooLarge, Code: "resource_exhausted", Message: "Memory exceeds maximum size"}
	}
	return data, nil
}

func sessionMemoryActor(sessionExternalID string) db.MemoryActor {
	return db.MemoryActor{Type: db.MemoryActorTypeSession, SessionID: sessionExternalID}
}

func sha256HexBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func ptrString(value string) *string {
	return &value
}

func ptrInt64(value int64) *int64 {
	return &value
}

func memoryFilePayload(record db.Memory, filesystemID, filestorePath, mediaType string) filesystemFilePayload {
	mediaType = normalizeMediaType(mediaType)
	if mediaType == "" {
		mediaType = "text/plain"
	}
	return filesystemFilePayload{
		File: filePayload{
			UUID:             record.UUID,
			CreatedAt:        formatTimestamp(record.CreatedAt),
			Size:             protoInt64(record.ContentSizeBytes),
			MediaType:        mediaType,
			EntryTaggedID:    record.ExternalID,
			DetectedMimeType: mediaType,
			Downloadable:     true,
			FilesystemID:     filesystemID,
		},
		FilesystemID: filesystemID,
		Path:         filestorePath,
	}
}

func virtualMemoryDirectory(filesystemID, path string, createdAt time.Time) directoryPayload {
	return directoryPayload{
		FilesystemID: filesystemID,
		Path:         path,
		CreatedAt:    formatTimestamp(createdAt),
	}
}
