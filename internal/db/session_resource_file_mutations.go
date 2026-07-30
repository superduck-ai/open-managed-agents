package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

const moveFilestoreFileResultQuery = `
	where workspace_uuid = :workspace_uuid
		and session_uuid = :session_uuid
		and id = :entry_id
		and deleted_at is null
`

// MakeFilestoreDirectory 创建目录；MakeParents 为真时整条父链在同一事务内完成。
func (d *DB) MakeFilestoreDirectory(ctx context.Context, input MakeFilestoreDirectoryInput) (SessionResourceFile, error) {
	if err := validateFilestorePath(input.Path); err != nil {
		return SessionResourceFile{}, err
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return SessionResourceFile{}, err
	}
	defer tx.Rollback()

	if input.Path == "/" {
		if err := tx.Commit(); err != nil {
			return SessionResourceFile{}, err
		}
		return virtualFilestoreRoot(filesystem), nil
	}
	if !input.MakeParents {
		if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.Path)); err != nil {
			return SessionResourceFile{}, err
		}
	}

	var directory SessionResourceFile
	paths := []string{input.Path}
	if input.MakeParents {
		paths = filestoreDirectoryChain(input.Path)
	}
	for _, directoryPath := range paths {
		directory, err = ensureFilestoreDirectoryTx(ctx, tx, input.WorkspaceID, filesystem, directoryPath, input.Now)
		if err != nil {
			return SessionResourceFile{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SessionResourceFile{}, err
	}
	return directory, nil
}

// PutFilestoreFile 将对象元数据写入命名空间，并与配额核算、旧对象清理任务及
// 孤儿哨兵取消保持同一事务边界。
func (d *DB) PutFilestoreFile(ctx context.Context, input PutFilestoreFileInput) (FilestoreMutationResult, error) {
	if err := validateFilestoreFileWrite(input.Path, input.Blob); err != nil {
		return FilestoreMutationResult{}, err
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback()

	result, err := putFilestoreFileTx(ctx, tx, filesystem, putFilestoreFileTxInput{
		WorkspaceID:                input.WorkspaceID,
		Path:                       input.Path,
		Blob:                       input.Blob,
		OverwriteExisting:          input.OverwriteExisting,
		OrphanCleanupJobExternalID: input.OrphanCleanupJobExternalID,
		WorkspaceStorageLimitBytes: input.WorkspaceStorageLimitBytes,
		Now:                        input.Now,
	})
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return result, nil
}

// CopyFilestoreFile 绑定已经由对象存储复制完成的新版本，并校验源对象仍是调用方读取的版本。
func (d *DB) CopyFilestoreFile(ctx context.Context, input CopyFilestoreFileInput) (FilestoreMutationResult, error) {
	if err := validateFilestorePath(input.SourcePath); err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := validateFilestorePath(input.DestinationPath); err != nil {
		return FilestoreMutationResult{}, err
	}
	if input.SourcePath == "/" || input.DestinationPath == "/" || strings.TrimSpace(input.DestinationS3Bucket) == "" || strings.TrimSpace(input.DestinationS3Key) == "" {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback()

	source, err := getActiveSessionResourceFileForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != SessionResourceFileKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	if source.ReferencesSourceFile() {
		// Input Resource 禁止通用 Filestore mutation。CopyFilestoreFile 的语义是
		// 把对象存储端已完成的 server-side copy 落库为 Owned File，
		// 不允许把 Source File 隐式物化为新的 Owned File。
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	if input.ExpectedSourceS3Key != "" && filestoreString(source.S3Key) != input.ExpectedSourceS3Key {
		// 对象复制发生在数据库事务之外；以对象键和版本号作乐观锁，拒绝陈旧副本落库。
		return FilestoreMutationResult{}, ErrVersionConflict
	}
	if input.ExpectedSourceS3VersionID != "" && filestoreString(source.S3VersionID) != input.ExpectedSourceS3VersionID {
		return FilestoreMutationResult{}, ErrVersionConflict
	}
	blob := filestoreBlobFromEntry(source)
	blob.S3Bucket = input.DestinationS3Bucket
	blob.S3Key = input.DestinationS3Key
	blob.S3ETag = input.DestinationS3ETag
	blob.S3VersionID = input.DestinationS3VersionID
	result, err := putFilestoreFileTx(ctx, tx, filesystem, putFilestoreFileTxInput{
		WorkspaceID:                input.WorkspaceID,
		Path:                       input.DestinationPath,
		Blob:                       blob,
		OverwriteExisting:          input.OverwriteExisting,
		OrphanCleanupJobExternalID: input.OrphanCleanupJobExternalID,
		WorkspaceStorageLimitBytes: input.WorkspaceStorageLimitBytes,
		Now:                        input.Now,
	})
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return result, nil
}

// MoveFilestoreFile 原子移动文件路径，可选覆盖目标文件；底层对象键保持不变。
func (d *DB) MoveFilestoreFile(ctx context.Context, input MoveFilestoreFileInput) (FilestoreMutationResult, error) {
	if err := validateFilestoreMovePaths(input.SourcePath, input.DestinationPath); err != nil {
		return FilestoreMutationResult{}, err
	}
	if input.SourcePath == "/" || input.DestinationPath == "/" {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback()

	source, err := getActiveSessionResourceFileForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != SessionResourceFileKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	if source.ReferencesSourceFile() {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	if input.SourcePath == input.DestinationPath {
		if err := tx.Commit(); err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{Node: source}, nil
	}
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.DestinationPath)); err != nil {
		return FilestoreMutationResult{}, err
	}

	var cleanupJobs []FilestoreObjectCleanupJob
	destination, found, err := getSessionResourceFileForMutation(ctx, tx, filesystem, input.DestinationPath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if found {
		if destination.ReferencesSourceFile() {
			return FilestoreMutationResult{}, ErrPreconditionFailed
		}
		if destination.Kind != SessionResourceFileKindFile {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
		if !input.OverwriteExisting {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
		job, enqueued, err := enqueueOwnedSessionResourceFileCleanupJobTx(ctx, tx, sessionResourceFileCleanupScope{
			WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
		}, destination, "move_overwrite", input.Now)
		if err != nil {
			return FilestoreMutationResult{}, err
		}
		if enqueued {
			cleanupJobs = append(cleanupJobs, job)
		}
		if err := retireSessionResourceFileTx(
			ctx, tx, input.WorkspaceID, destination.ID, input.Now,
		); err != nil {
			return FilestoreMutationResult{}, err
		}
		removedBytes := destination.OwnedBytes()
		if removedBytes > 0 {
			if err := applyWorkspaceStorageDeltaSQLXTx(
				ctx, tx, input.WorkspaceID, 0, -removedBytes, 0,
			); err != nil {
				return FilestoreMutationResult{}, err
			}
		}
	}

	if _, err := namedExecContext(ctx, tx, `
		update session_resources
		set path = :destination_path, parent_path = :destination_parent_path,
			updated_at = :now
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and id = :entry_id and deleted_at is null
	`, map[string]any{
		"workspace_uuid":          filesystem.WorkspaceUUID,
		"session_uuid":            filesystem.SessionUUID,
		"entry_id":                source.ID,
		"destination_path":        input.DestinationPath,
		"destination_parent_path": filestoreParentPath(input.DestinationPath),
		"now":                     input.Now,
	}); err != nil {
		return FilestoreMutationResult{}, err
	}
	resultArguments := sessionResourceFileMutationArguments(filesystem, input.SourcePath)
	resultArguments["entry_id"] = source.ID
	moved, err := getSessionResourceFileSQLX(
		ctx,
		tx,
		sessionResourceFileSelectSQL()+moveFilestoreFileResultQuery,
		resultArguments,
	)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Node: moved, CleanupJobs: cleanupJobs}, nil
}

// MoveFilestoreDirectory 原子重写目录及全部后代路径，并拒绝移入自身子树。
func (d *DB) MoveFilestoreDirectory(ctx context.Context, input MoveFilestoreDirectoryInput) (FilestoreMutationResult, error) {
	if err := validateFilestoreMovePaths(input.SourcePath, input.DestinationPath); err != nil {
		return FilestoreMutationResult{}, err
	}
	if input.SourcePath == "/" || input.DestinationPath == "/" ||
		filestorePathIsDescendant(input.SourcePath, input.DestinationPath) {
		return FilestoreMutationResult{}, ErrFilestoreInvalidMove
	}
	if err := validateFilestoreDirectoryMoveRoots(input.SourcePath, input.DestinationPath); err != nil {
		return FilestoreMutationResult{}, err
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback()

	source, err := getActiveSessionResourceFileForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != SessionResourceFileKindDirectory {
		return FilestoreMutationResult{}, ErrFilestoreNotDirectory
	}
	if input.SourcePath == input.DestinationPath {
		if err := tx.Commit(); err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{Node: source}, nil
	}
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.DestinationPath)); err != nil {
		return FilestoreMutationResult{}, err
	}
	var maxMovedPathBytes int
	// 在批量更新前按字节预演最长目标路径，避免中途触发约束而留下难以解释的错误。
	moveArguments := map[string]any{
		"workspace_id":            input.WorkspaceID,
		"session_uuid":            filesystem.SessionUUID,
		"workspace_uuid":          filesystem.WorkspaceUUID,
		"source_path":             input.SourcePath,
		"destination_path":        input.DestinationPath,
		"destination_parent_path": filestoreParentPath(input.DestinationPath),
		"now":                     input.Now,
	}
	if err := namedGetContext(ctx, tx, &maxMovedPathBytes, `
		select coalesce(max(
			octet_length(CAST(:destination_path AS text))
				+ octet_length(path)
				- octet_length(CAST(:source_path AS text))
		), 0)
		from session_resources
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and deleted_at is null
			and (
				path = :source_path
				or left(path, char_length(:source_path) + 1) = :source_path || '/'
			)
	`, moveArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if maxMovedPathBytes > filestoreMaxPathBytes {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	var containsInput bool
	if err := namedGetContext(ctx, tx, &containsInput, `
		select exists (
			select 1 from session_resources
			where workspace_uuid = CAST(:workspace_uuid AS uuid)
				and session_uuid = CAST(:session_uuid AS uuid)
				and deleted_at is null and payload is not null
				and (
					path = :source_path
					or left(path, char_length(:source_path) + 1) = :source_path || '/'
				)
		)
	`, moveArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if containsInput {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	var conflictingID int64
	err = namedGetContext(ctx, tx, &conflictingID, `
		select id
		from session_resources
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and deleted_at is null
			and (expires_at is null or expires_at > now())
			and (
				path = :destination_path
				or left(path, char_length(:destination_path) + 1) = :destination_path || '/'
			)
		limit 1
	`, moveArguments)
	if err == nil {
		return FilestoreMutationResult{}, ErrFilestorePathExists
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return FilestoreMutationResult{}, err
	}
	cleanupJobs, retiredBytes, err := retireExpiredFilestoreSubtreeTx(
		ctx, tx, sessionResourceFileCleanupScope{
			WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
		}, filesystem, input.DestinationPath, input.Now,
	)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if retiredBytes > 0 {
		if err := applyWorkspaceStorageDeltaSQLXTx(ctx, tx, input.WorkspaceID, 0, -retiredBytes, 0); err != nil {
			return FilestoreMutationResult{}, err
		}
	}
	// 利用前缀替换一次更新整棵子树；文件内容按稳定对象键寻址，无须随路径迁移。
	if _, err := namedExecContext(ctx, tx, `
		update session_resources
		set path = :destination_path || substring(path from char_length(:source_path) + 1),
			parent_path = case
				when path = :source_path then :destination_parent_path
				else :destination_path || substring(parent_path from char_length(:source_path) + 1)
			end,
			updated_at = :now
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and deleted_at is null
			and (
				path = :source_path
				or left(path, char_length(:source_path) + 1) = :source_path || '/'
			)
	`, moveArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	moved, err := getSessionResourceFileSQLX(ctx, tx, sessionResourceFileSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and path = :destination_path
			and deleted_at is null
	`, moveArguments)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Node: moved, CleanupJobs: cleanupJobs}, nil
}

// RemoveFilestoreFile 软删除逻辑文件引用；仅为 Filestore 自有对象创建清理任务。
func (d *DB) RemoveFilestoreFile(ctx context.Context, input RemoveSessionResourceFileInput) (FilestoreMutationResult, error) {
	if err := validateFilestorePath(input.Path); err != nil || input.Path == "/" {
		if err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback()
	entry, err := getActiveSessionResourceFileForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if entry.Kind != SessionResourceFileKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	if entry.ReferencesSourceFile() {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	job, enqueued, err := enqueueOwnedSessionResourceFileCleanupJobTx(ctx, tx, sessionResourceFileCleanupScope{
		WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
	}, entry, "remove_file", input.Now)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := retireSessionResourceFileTx(ctx, tx, input.WorkspaceID, entry.ID, input.Now); err != nil {
		return FilestoreMutationResult{}, err
	}
	if removedBytes := entry.OwnedBytes(); removedBytes > 0 {
		if err := applyWorkspaceStorageDeltaSQLXTx(
			ctx, tx, input.WorkspaceID, 0, -removedBytes, 0,
		); err != nil {
			return FilestoreMutationResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	result := FilestoreMutationResult{Node: entry}
	if enqueued {
		result.CleanupJobs = []FilestoreObjectCleanupJob{job}
	}
	return result, nil
}

// RemoveFilestoreDirectory 软删除目录视图；递归删除只清理子树内 Filestore 自有对象。
func (d *DB) RemoveFilestoreDirectory(ctx context.Context, input RemoveFilestoreDirectoryInput) (FilestoreMutationResult, error) {
	if err := validateFilestorePath(input.Path); err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := validateFilestoreDirectoryRemovalRoot(input.Path); err != nil {
		return FilestoreMutationResult{}, err
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback()
	entry, err := getActiveSessionResourceFileForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if entry.Kind != SessionResourceFileKindDirectory {
		return FilestoreMutationResult{}, ErrFilestoreNotDirectory
	}
	var childCount int
	entryArguments := map[string]any{
		"workspace_id":   input.WorkspaceID,
		"workspace_uuid": filesystem.WorkspaceUUID,
		"session_uuid":   filesystem.SessionUUID,
		"entry_path":     input.Path,
		"now":            input.Now,
	}
	if err := namedGetContext(ctx, tx, &childCount, `
		select count(*) from session_resources
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and parent_path = :entry_path
			and deleted_at is null and (expires_at is null or expires_at > now())
	`, entryArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if childCount > 0 && !input.Recursive {
		return FilestoreMutationResult{}, ErrFilestoreDirectoryNotEmpty
	}
	var containsInput bool
	if err := namedGetContext(ctx, tx, &containsInput, `
		select exists (
			select 1 from session_resources
			where workspace_uuid = CAST(:workspace_uuid AS uuid)
				and session_uuid = CAST(:session_uuid AS uuid)
				and deleted_at is null and payload is not null
				and (
					path = :entry_path
					or left(path, char_length(:entry_path) + 1) = :entry_path || '/'
				)
		)
	`, entryArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if containsInput {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	cleanupJobs, removedBytes, err := enqueueFilestoreSubtreeCleanupJobsTx(ctx, tx, sessionResourceFileCleanupScope{
		WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
	}, filesystem, input.Path, input.Now)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update files file
		set deleted_at = coalesce(file.deleted_at, :now)
		where file.workspace_id = :workspace_id
			and file.uuid in (
				select resource.file_uuid from session_resources resource
				where resource.workspace_uuid = CAST(:workspace_uuid AS uuid)
					and resource.session_uuid = CAST(:session_uuid AS uuid)
					and resource.deleted_at is null
					and resource.payload is null
					and resource.resource_type = 'file'
					and (
						resource.path = :entry_path
						or left(resource.path, char_length(:entry_path) + 1) = :entry_path || '/'
					)
			)
	`, entryArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update session_resources
		set deleted_at = :now, updated_at = :now
		where workspace_uuid = CAST(:workspace_uuid AS uuid)
			and session_uuid = CAST(:session_uuid AS uuid)
			and deleted_at is null
			and (
				path = :entry_path
				or left(path, char_length(:entry_path) + 1) = :entry_path || '/'
			)
	`, entryArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if removedBytes > 0 {
		if err := applyWorkspaceStorageDeltaSQLXTx(ctx, tx, input.WorkspaceID, 0, -removedBytes, 0); err != nil {
			return FilestoreMutationResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Node: entry, CleanupJobs: cleanupJobs}, nil
}
