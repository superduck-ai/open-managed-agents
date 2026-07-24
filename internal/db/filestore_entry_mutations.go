package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MakeFilestoreDirectory 创建目录；MakeParents 为真时整条父链在同一事务内完成。
func (d *DB) MakeFilestoreDirectory(ctx context.Context, input MakeFilestoreDirectoryInput) (FilestoreEntry, error) {
	if err := validateFilestorePath(input.Path); err != nil {
		return FilestoreEntry{}, err
	}
	input.Now = filestoreNow(input.Now)
	// 创建目录可能复用一个已过期的文件路径，因此也要先取得工作区用量锁。
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreEntry{}, err
	}
	defer tx.Rollback()

	if input.Path == "/" {
		if err := tx.Commit(); err != nil {
			return FilestoreEntry{}, err
		}
		return virtualFilestoreRoot(filesystem), nil
	}
	if !input.MakeParents {
		if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.Path)); err != nil {
			return FilestoreEntry{}, err
		}
	}

	var directory FilestoreEntry
	paths := []string{input.Path}
	if input.MakeParents {
		paths = filestoreDirectoryChain(input.Path)
	}
	for _, directoryPath := range paths {
		directory, err = ensureFilestoreDirectoryTx(ctx, tx, input.WorkspaceID, filesystem, directoryPath, input.Now)
		if err != nil {
			return FilestoreEntry{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FilestoreEntry{}, err
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

	source, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != FilestoreEntryKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
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

	source, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != FilestoreEntryKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	if input.SourcePath == input.DestinationPath {
		if err := tx.Commit(); err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{Entry: source}, nil
	}
	if source.ManagedBy != nil || source.ManagedResourceExternalID != nil {
		return FilestoreMutationResult{}, ErrFilestoreInvalidMove
	}
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.DestinationPath)); err != nil {
		return FilestoreMutationResult{}, err
	}

	var cleanupJobs []FilestoreObjectCleanupJob
	destination, found, err := getFilestoreEntryForMutation(ctx, tx, filesystem, input.DestinationPath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	var databaseNow time.Time
	if err := tx.GetContext(ctx, &databaseNow, `select now()`); err != nil {
		return FilestoreMutationResult{}, err
	}
	if found {
		if destination.SourceFileUUID != nil {
			return FilestoreMutationResult{}, ErrPreconditionFailed
		}
		if !filestoreEntryExpired(destination, databaseNow) {
			if destination.Kind != FilestoreEntryKindFile {
				return FilestoreMutationResult{}, ErrFilestorePathExists
			}
			if !input.OverwriteExisting {
				return FilestoreMutationResult{}, ErrFilestorePathExists
			}
		}
		if destination.Kind == FilestoreEntryKindFile {
			job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, filestoreEntryCleanupScope{
				WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
			}, destination, "move_overwrite", input.Now)
			if err != nil {
				return FilestoreMutationResult{}, err
			}
			cleanupJobs = append(cleanupJobs, job)
		}
		if _, err := namedExecContext(ctx, tx, `
			update filestore_entries
			set deleted_at = :now, updated_at = :now
			where workspace_uuid = :workspace_uuid
				and filesystem_uuid = :filesystem_uuid
				and id = :entry_id
				and deleted_at is null
		`, map[string]any{
			"workspace_uuid":  filesystem.WorkspaceUUID,
			"filesystem_uuid": filesystem.UUID,
			"entry_id":        destination.ID,
			"now":             input.Now,
		}); err != nil {
			return FilestoreMutationResult{}, err
		}
		if destination.Kind == FilestoreEntryKindFile {
			if err := applyWorkspaceStorageDeltaSQLXTx(
				ctx, tx, input.WorkspaceID, 0, -filestoreInt64(destination.SizeBytes), 0,
			); err != nil {
				return FilestoreMutationResult{}, err
			}
		}
	}

	moved, err := getFilestoreEntrySQLX(ctx, tx, `
		update filestore_entries
		set path = :destination_path,
			parent_path = :destination_parent_path,
			updated_at = :now
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and id = :entry_id
			and deleted_at is null
		returning `+filestoreEntryColumns()+`
	`, map[string]any{
		"workspace_uuid":          filesystem.WorkspaceUUID,
		"filesystem_uuid":         filesystem.UUID,
		"entry_id":                source.ID,
		"destination_path":        input.DestinationPath,
		"destination_parent_path": filestoreParentPath(input.DestinationPath),
		"now":                     input.Now,
	})
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Entry: moved, CleanupJobs: cleanupJobs}, nil
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

	source, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != FilestoreEntryKindDirectory {
		return FilestoreMutationResult{}, ErrFilestoreNotDirectory
	}
	if input.SourcePath == input.DestinationPath {
		if err := tx.Commit(); err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{Entry: source}, nil
	}
	containsManagedEntry, err := filestoreSubtreeContainsManagedEntryTx(
		ctx,
		tx,
		filesystem,
		input.SourcePath,
	)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if containsManagedEntry {
		return FilestoreMutationResult{}, ErrFilestoreInvalidMove
	}
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.DestinationPath)); err != nil {
		return FilestoreMutationResult{}, err
	}
	var maxMovedPathBytes int
	// 在批量更新前按字节预演最长目标路径，避免中途触发约束而留下难以解释的错误。
	moveArguments := map[string]any{
		"workspace_uuid":          filesystem.WorkspaceUUID,
		"filesystem_uuid":         filesystem.UUID,
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
		from filestore_entries
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
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
	var conflictingID int64
	err = namedGetContext(ctx, tx, &conflictingID, `
		select id
		from filestore_entries
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
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
		ctx, tx, filestoreEntryCleanupScope{
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
		update filestore_entries
		set path = :destination_path || substring(path from char_length(:source_path) + 1),
			parent_path = case
				when path = :source_path then :destination_parent_path
				else :destination_path || substring(parent_path from char_length(:source_path) + 1)
			end,
			updated_at = :now
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and deleted_at is null
			and (
				path = :source_path
				or left(path, char_length(:source_path) + 1) = :source_path || '/'
			)
	`, moveArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	moved, err := getFilestoreEntrySQLX(ctx, tx, filestoreEntrySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and path = :destination_path
			and deleted_at is null
	`, moveArguments)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Entry: moved, CleanupJobs: cleanupJobs}, nil
}

// RemoveFilestoreFile 软删除文件，并在同一事务内为其精确对象版本创建清理任务。
func (d *DB) RemoveFilestoreFile(ctx context.Context, input RemoveFilestoreEntryInput) (FilestoreMutationResult, error) {
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
	entry, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if entry.Kind != FilestoreEntryKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	if entry.SourceFileUUID != nil {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, filestoreEntryCleanupScope{
		WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
	}, entry, "remove_file", input.Now)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update filestore_entries
		set deleted_at = :now, updated_at = :now
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and id = :entry_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"entry_id":        entry.ID,
		"now":             input.Now,
	}); err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := applyWorkspaceStorageDeltaSQLXTx(
		ctx, tx, input.WorkspaceID, 0, -filestoreInt64(entry.SizeBytes), 0,
	); err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Entry: entry, CleanupJobs: []FilestoreObjectCleanupJob{job}}, nil
}

// RemoveFilestoreDirectory 软删除目录；递归删除时为子树内每个文件创建对象清理任务。
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
	entry, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if entry.Kind != FilestoreEntryKindDirectory {
		return FilestoreMutationResult{}, ErrFilestoreNotDirectory
	}
	containsManagedEntry, err := filestoreSubtreeContainsManagedEntryTx(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if containsManagedEntry {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}

	var childCount int
	entryArguments := map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"entry_path":      input.Path,
		"now":             input.Now,
	}
	if err := namedGetContext(ctx, tx, &childCount, `
		select count(*) from filestore_entries
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and parent_path = :entry_path
			and deleted_at is null and (expires_at is null or expires_at > now())
	`, entryArguments); err != nil {
		return FilestoreMutationResult{}, err
	}
	if childCount > 0 && !input.Recursive {
		return FilestoreMutationResult{}, ErrFilestoreDirectoryNotEmpty
	}
	cleanupJobs, removedBytes, err := enqueueFilestoreSubtreeCleanupJobsTx(ctx, tx, filestoreEntryCleanupScope{
		WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
	}, filesystem, input.Path, input.Now)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		update filestore_entries
		set deleted_at = :now, updated_at = :now
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
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
	return FilestoreMutationResult{Entry: entry, CleanupJobs: cleanupJobs}, nil
}
