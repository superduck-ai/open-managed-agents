package db

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/google/uuid"
	"github.com/superduck-ai/yourbatis"
)

func withFilestoreNamespaceMutation[T any](
	ctx context.Context,
	database *yourbatis.DB,
	workspaceUUID string,
	filesystemUUID string,
	fn func(yourbatis.Executor, FilestoreFilesystem) (T, error),
) (T, error) {
	var result T
	err := database.Transaction(ctx, func(executor yourbatis.Executor) error {
		storageMapper := NewWorkspaceStorageUsageMapper(executor)
		// 即使目录操作通常不改变容量，也可能替换到期文件；统一锁序比事后升级锁更安全。
		if err := storageMapper.LockWorkspace(ctx, workspaceUUID); err != nil {
			return err
		}
		filesystemMapper := NewFilestoreFilesystemMapper(executor)
		row, found, err := filesystemMapper.FindFilesystemForMutation(ctx, workspaceUUID, filesystemUUID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		filesystem, err := row.filesystem()
		if err != nil {
			return err
		}
		if err := filesystemMapper.LockFilesystem(ctx, filesystem.UUID); err != nil {
			return err
		}
		result, err = fn(executor, filesystem)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}

func (d *DB) resolveFilestoreDirectoryForRead(ctx context.Context, workspaceUUID, filesystemUUID string, directoryPath string) (FilestoreFilesystem, error) {
	filesystemMapper := NewFilestoreFilesystemMapper(d.mapperDB)
	filesystemRow, found, err := filesystemMapper.FindFilesystemByUUID(
		ctx,
		workspaceUUID,
		filesystemUUID,
	)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if !found {
		return FilestoreFilesystem{}, ErrNotFound
	}
	filesystem, err := filesystemRow.filesystem()
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if directoryPath == "/" {
		return filesystem, nil
	}
	entry, err := getActiveSessionResourceFile(ctx, d.mapperDB, filesystem, directoryPath)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if entry.Kind != SessionResourceFileKindDirectory {
		return FilestoreFilesystem{}, ErrFilestoreNotDirectory
	}
	return filesystem, nil
}

func requireFilestoreDirectoryTx(ctx context.Context, tx yourbatis.Executor, filesystem FilestoreFilesystem, directoryPath string) error {
	if directoryPath == "/" {
		return nil
	}
	entry, err := getActiveSessionResourceFileForMutation(ctx, tx, filesystem, directoryPath)
	if errors.Is(err, ErrNotFound) {
		return ErrFilestoreParentMissing
	}
	if err != nil {
		return err
	}
	if entry.Kind != SessionResourceFileKindDirectory {
		return ErrFilestoreNotDirectory
	}
	return nil
}

func getSessionResourceFileForMutation(ctx context.Context, tx yourbatis.Executor, filesystem FilestoreFilesystem, entryPath string) (SessionResourceFile, bool, error) {
	mapper := NewSessionResourceFileMapper(tx)
	row, found, err := mapper.FindResourceFile(ctx, sessionResourcePathParams{
		WorkspaceUUID: filesystem.WorkspaceUUID,
		SessionUUID:   filesystem.SessionUUID,
		EntryPath:     entryPath,
	})
	if err != nil {
		return SessionResourceFile{}, false, err
	}
	if !found {
		return SessionResourceFile{}, false, nil
	}
	entry, err := row.entry()
	return entry, err == nil, err
}

func getActiveSessionResourceFileForMutation(ctx context.Context, tx yourbatis.Executor, filesystem FilestoreFilesystem, entryPath string) (SessionResourceFile, error) {
	return getActiveSessionResourceFile(ctx, tx, filesystem, entryPath)
}

func validateFilestorePath(value string) error {
	if err := filestorepath.Validate(value, true); err != nil {
		return ErrPreconditionFailed
	}
	return nil
}

func validateFilestoreMovePaths(source, destination string) error {
	if err := validateFilestorePath(source); err != nil {
		return err
	}
	return validateFilestorePath(destination)
}

func validateFilestoreFileWrite(entryPath string, blob FilestoreFileBlob) error {
	if err := validateFilestorePath(entryPath); err != nil {
		return err
	}
	if entryPath == "/" || blob.SizeBytes < 0 || strings.TrimSpace(blob.MediaType) == "" || strings.TrimSpace(blob.MD5) == "" || len(blob.SHA256) != 64 || strings.TrimSpace(blob.S3Bucket) == "" || strings.TrimSpace(blob.S3Key) == "" {
		return ErrPreconditionFailed
	}
	if err := validateFilestoreJSONObject(blob.Metadata); err != nil {
		return err
	}
	return validateFilestoreJSONObject(blob.AuthorizationMetadata)
}

func validateFilestoreJSONObject(value json.RawMessage) error {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if !json.Valid(trimmed) || trimmed[0] != '{' {
		return ErrPreconditionFailed
	}
	return nil
}

func filestoreParentPath(value string) string {
	return filestorepath.Parent(value)
}

func filestoreDirectoryChain(value string) []string {
	return filestorepath.DirectoryChain(value)
}

func filestorePathIsDescendant(parentPath, candidatePath string) bool {
	return filestorepath.IsDescendant(candidatePath, parentPath)
}

func filestoreNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

// 如果这个 directoryPath 已经存在，而且本来就是目录，就直接返回现有目录。
// 如果这个路径被非目录 entry 占着，就返回 ErrFilestorePathExists。
// 如果这个路径根本不存在，就插入一条新的 directory entry。
func ensureFilestoreDirectoryTx(ctx context.Context, tx yourbatis.Executor, filesystem FilestoreFilesystem, directoryPath string, now time.Time) (SessionResourceFile, error) {
	existing, found, err := getSessionResourceFileForMutation(ctx, tx, filesystem, directoryPath)
	if err != nil {
		return SessionResourceFile{}, err
	}
	if found {
		if existing.Kind == SessionResourceFileKindDirectory {
			return existing, nil
		}
		return SessionResourceFile{}, ErrFilestorePathExists
	}
	resourceUUID, resourceExternalID, err := newSessionResourceIdentity()
	if err != nil {
		return SessionResourceFile{}, err
	}
	resourceMapper := NewSessionResourceMapper(tx)
	_, err = resourceMapper.InsertDirectory(ctx, sessionResourceDirectoryInsertParams{
		ResourceUUID:       resourceUUID,
		ResourceExternalID: resourceExternalID,
		OrganizationUUID:   filesystem.OrganizationUUID,
		WorkspaceUUID:      filesystem.WorkspaceUUID,
		SessionUUID:        filesystem.SessionUUID,
		EntryPath:          directoryPath,
		ParentPath:         filestoreParentPath(directoryPath),
		Now:                now,
	})
	if err := mapSessionNamespaceInsertError(err); err != nil {
		return SessionResourceFile{}, err
	}
	fileMapper := NewSessionResourceFileMapper(tx)
	row, err := fileMapper.GetResourceFileByUUID(ctx, sessionResourceIdentityParams{
		WorkspaceUUID: filesystem.WorkspaceUUID,
		SessionUUID:   filesystem.SessionUUID,
		ResourceUUID:  resourceUUID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SessionResourceFile{}, ErrNotFound
	}
	if err != nil {
		return SessionResourceFile{}, err
	}
	return row.entry()
}

type putFilestoreFileTxInput struct {
	WorkspaceUUID              string
	Path                       string
	Blob                       FilestoreFileBlob
	OverwriteExisting          bool
	OrphanCleanupJobExternalID string
	WorkspaceStorageLimitBytes int64
	Now                        time.Time
}

func putFilestoreFileTx(ctx context.Context, tx yourbatis.Executor, filesystem FilestoreFilesystem, input putFilestoreFileTxInput) (FilestoreMutationResult, error) {
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.Path)); err != nil {
		return FilestoreMutationResult{}, err
	}
	existing, found, err := getSessionResourceFileForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	var oldSize int64
	if found && existing.Kind == SessionResourceFileKindFile {
		oldSize = existing.OwnedBytes()
	}
	if found {
		if existing.Kind != SessionResourceFileKindFile {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
		if existing.ReferencesSourceFile() {
			return FilestoreMutationResult{}, ErrPreconditionFailed
		}
		if !input.OverwriteExisting {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
	}
	storageDelta := input.Blob.SizeBytes - oldSize
	if err := applyWorkspaceStorageDeltaTx(
		ctx, tx, input.WorkspaceUUID, 0, storageDelta, input.WorkspaceStorageLimitBytes,
	); err != nil {
		return FilestoreMutationResult{}, err
	}

	var cleanupJobs []FilestoreObjectCleanupJob
	if found && existing.Kind == SessionResourceFileKindFile && !sameFilestoreObject(existing, input.Blob) {
		job, enqueued, err := enqueueOwnedSessionResourceFileCleanupJobTx(ctx, tx, sessionResourceFileCleanupScope{
			WorkspaceUUID: input.WorkspaceUUID, FilesystemUUID: filesystem.UUID,
		}, existing, "file_replaced", input.Now)
		if err != nil {
			return FilestoreMutationResult{}, err
		}
		if enqueued {
			cleanupJobs = append(cleanupJobs, job)
		}
	}
	entry, err := writeFilestoreFileTx(ctx, tx, filesystem, existing, found, input)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if input.OrphanCleanupJobExternalID != "" {
		// 只有桶、键、版本均与待落库对象吻合时才取消哨兵，防止错绑任务掩盖孤儿。
		if err := cancelAttachedFilestoreObjectCleanupJobTx(ctx, tx, input.WorkspaceUUID, input.OrphanCleanupJobExternalID, input.Blob); err != nil {
			return FilestoreMutationResult{}, err
		}
	}
	return FilestoreMutationResult{Node: entry, CleanupJobs: cleanupJobs}, nil
}

func writeFilestoreFileTx(ctx context.Context, tx yourbatis.Executor, filesystem FilestoreFilesystem, existing SessionResourceFile, found bool, input putFilestoreFileTxInput) (SessionResourceFile, error) {
	params := filestoreFileWriteMapperParams(filesystem, input)
	fileMapper := NewFileMapper(tx)
	resourceMapper := NewSessionResourceMapper(tx)
	viewMapper := NewSessionResourceFileMapper(tx)
	if found {
		params.ResourceUUID = existing.UUID
		if err := fileMapper.UpdateOwnedFile(ctx, params); err != nil {
			return SessionResourceFile{}, err
		}
		if err := resourceMapper.UpdateResourceFile(ctx, params); err != nil {
			return SessionResourceFile{}, err
		}
		row, err := viewMapper.GetResourceFileByUUID(ctx, sessionResourceIdentityParams{
			WorkspaceUUID: params.WorkspaceUUID,
			SessionUUID:   params.SessionUUID,
			ResourceUUID:  params.ResourceUUID,
		})
		if err != nil {
			return SessionResourceFile{}, err
		}
		return row.entry()
	}
	fileUUID, fileExternalID, err := newFileIdentity()
	if err != nil {
		return SessionResourceFile{}, err
	}
	resourceUUID, resourceExternalID, err := newSessionResourceIdentity()
	if err != nil {
		return SessionResourceFile{}, err
	}
	params.FileUUID = fileUUID
	params.FileExternalID = fileExternalID
	params.ResourceUUID = resourceUUID
	params.ResourceExternalID = resourceExternalID
	_, err = resourceMapper.InsertOwnedFileAndResource(ctx, params)
	if err := mapSessionNamespaceInsertError(err); err != nil {
		return SessionResourceFile{}, err
	}
	row, err := viewMapper.GetResourceFileByUUID(ctx, sessionResourceIdentityParams{
		WorkspaceUUID: params.WorkspaceUUID,
		SessionUUID:   params.SessionUUID,
		ResourceUUID:  params.ResourceUUID,
	})
	if err != nil {
		return SessionResourceFile{}, err
	}
	return row.entry()
}

func mapSessionNamespaceInsertError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if isUniqueViolation(err) {
		return ErrFilestorePathExists
	}
	return err
}

// newSessionResourceIdentity 在应用层生成 Session Resource 的 uuid 与 sesrsc_ external ID，
// 使 namespace INSERT 只持久化已生成的身份，而不在 SQL 层拼接 gen_random_uuid + concat。
func newSessionResourceIdentity() (resourceUUID, resourceExternalID string, err error) {
	resourceExternalID, err = ids.New("sesrsc_")
	if err != nil {
		return "", "", err
	}
	return uuid.NewString(), resourceExternalID, nil
}

// newFileIdentity 在应用层生成真实 File 的 uuid 与 file_ external ID。
func newFileIdentity() (fileUUID, fileExternalID string, err error) {
	fileExternalID, err = ids.New("file_")
	if err != nil {
		return "", "", err
	}
	return uuid.NewString(), fileExternalID, nil
}

func filestoreFileWriteMapperParams(filesystem FilestoreFilesystem, input putFilestoreFileTxInput) sessionResourceFileWriteParams {
	return sessionResourceFileWriteParams{
		OrganizationUUID:      filesystem.OrganizationUUID,
		WorkspaceUUID:         filesystem.WorkspaceUUID,
		EntryPath:             input.Path,
		Filename:              path.Base(input.Path),
		ParentPath:            filestoreParentPath(input.Path),
		SizeBytes:             input.Blob.SizeBytes,
		MediaType:             input.Blob.MediaType,
		DetectedMimeType:      filestoreNullableString(input.Blob.DetectedMimeType),
		Metadata:              string(filestoreJSONObject(input.Blob.Metadata)),
		AuthorizationMetadata: string(filestoreJSONObject(input.Blob.AuthorizationMetadata)),
		Tags:                  filestoreTags(input.Blob.Tags),
		Downloadable:          input.Blob.Downloadable,
		MD5:                   input.Blob.MD5,
		SHA256:                input.Blob.SHA256,
		S3Bucket:              input.Blob.S3Bucket,
		S3Key:                 input.Blob.S3Key,
		S3ETag:                filestoreNullableString(input.Blob.S3ETag),
		S3VersionID:           filestoreNullableString(input.Blob.S3VersionID),
		ExpiresAt:             input.Blob.ExpiresAt,
		CreatedByAPIKeyUUID:   filesystem.CreatedByAPIKeyUUID,
		SessionUUID:           filesystem.SessionUUID,
		Now:                   input.Now,
	}
}

func filestoreBlobFromEntry(entry SessionResourceFile) FilestoreFileBlob {
	return FilestoreFileBlob{
		SizeBytes:             filestoreInt64(entry.SizeBytes),
		MediaType:             filestoreString(entry.MediaType),
		DetectedMimeType:      filestoreString(entry.DetectedMimeType),
		Metadata:              copyRaw(entry.Metadata),
		AuthorizationMetadata: copyRaw(entry.AuthorizationMetadata),
		Tags:                  append([]string(nil), entry.Tags...),
		Downloadable:          entry.Downloadable,
		MD5:                   filestoreString(entry.MD5),
		SHA256:                filestoreString(entry.SHA256),
		S3Bucket:              filestoreString(entry.S3Bucket),
		S3Key:                 filestoreString(entry.S3Key),
		S3ETag:                filestoreString(entry.S3ETag),
		S3VersionID:           filestoreString(entry.S3VersionID),
		ExpiresAt:             entry.ExpiresAt,
	}
}

func sameFilestoreObject(entry SessionResourceFile, blob FilestoreFileBlob) bool {
	return filestoreString(entry.S3Bucket) == blob.S3Bucket && filestoreString(entry.S3Key) == blob.S3Key && filestoreString(entry.S3VersionID) == blob.S3VersionID
}

func retireSessionResourceFileTx(
	ctx context.Context,
	tx yourbatis.Executor,
	workspaceUUID string,
	resourceUUID string,
	retiredAt time.Time,
) error {
	params := sessionResourceRetireParams{
		ResourceUUID:  resourceUUID,
		WorkspaceUUID: workspaceUUID,
		RetiredAt:     retiredAt,
	}
	fileMapper := NewFileMapper(tx)
	if err := fileMapper.RetireOwnedFile(ctx, params); err != nil {
		return err
	}
	resourceMapper := NewSessionResourceMapper(tx)
	return resourceMapper.RetireResource(ctx, params)
}
