package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"

	"github.com/jackc/pgx/v5"
)

const (
	defaultFilestoreEntriesPageLimit = 100
	maxFilestoreEntriesPageLimit     = 1000
)

// GetFilestoreEntry 在工作区与文件系统双重边界内读取一个有效节点。
// 根目录不落表，由文件系统记录即时投影为虚拟目录。
func (d *DB) GetFilestoreEntry(ctx context.Context, workspaceID, filesystemID int64, entryPath string) (FilestoreEntry, error) {
	if err := validateFilestorePath(entryPath); err != nil {
		return FilestoreEntry{}, err
	}
	filesystem, err := getFilestoreFilesystemByIDSQLX(ctx, d.sql, workspaceID, filesystemID)
	if err != nil {
		return FilestoreEntry{}, err
	}
	if entryPath == "/" {
		return virtualFilestoreRoot(filesystem), nil
	}
	return getActiveFilestoreEntrySQLX(ctx, d.sql, filesystem, entryPath)
}

// ListFilestoreEntriesPage 以 (path, id) 为稳定排序键执行键集分页。
// 过期或软删除节点不会出现在结果中。
func (d *DB) ListFilestoreEntriesPage(ctx context.Context, params ListFilestoreEntriesPageParams) (FilestoreEntryPage, error) {
	if err := validateFilestorePath(params.DirectoryPath); err != nil {
		return FilestoreEntryPage{}, err
	}
	params.Limit = normalizeFilestoreEntriesPageLimit(params.Limit)
	filesystem, err := d.resolveFilestoreDirectoryForRead(ctx, params.WorkspaceID, params.FilesystemID, params.DirectoryPath)
	if err != nil {
		return FilestoreEntryPage{}, err
	}
	query, args := buildFilestoreEntriesPageQuery(filesystem, params)
	var rows []filestoreEntryRow
	if err := namedSelectContext(ctx, d.sql, &rows, query, args); err != nil {
		return FilestoreEntryPage{}, err
	}
	entries, err := filestoreEntriesFromSQLXRows(rows)
	if err != nil {
		return FilestoreEntryPage{}, err
	}
	return newFilestoreEntryPage(entries, params.Limit), nil
}

func normalizeFilestoreEntriesPageLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultFilestoreEntriesPageLimit
	case limit > maxFilestoreEntriesPageLimit:
		return maxFilestoreEntriesPageLimit
	default:
		return limit
	}
}

func buildFilestoreEntriesPageQuery(filesystem FilestoreFilesystem, params ListFilestoreEntriesPageParams) (string, map[string]any) {
	query := filestoreEntrySelectSQLX() + `
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and deleted_at is null
			and (expires_at is null or expires_at > now())
	`
	args := map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"fetch_limit":     params.Limit + 1,
	}
	if params.Recursive {
		// 在 Go 中补齐分隔符，确保 /foo 不会误包含 /foobar。
		query += " and left(path, char_length(:directory_prefix)) = :directory_prefix"
		args["directory_prefix"] = filestoreDirectoryPrefix(params.DirectoryPath)
	} else {
		query += " and parent_path = :directory_path"
		args["directory_path"] = params.DirectoryPath
	}
	if params.Cursor != nil {
		query += " and (path, id) > (:cursor_path, :cursor_id)"
		args["cursor_path"] = params.Cursor.Path
		args["cursor_id"] = params.Cursor.ID
	}
	// 多取一条只用于判定 HasMore；返回页仍严格遵守请求的 Limit。
	query += " order by path asc, id asc limit :fetch_limit"
	return query, args
}

func filestoreDirectoryPrefix(directoryPath string) string {
	if directoryPath == "/" {
		return directoryPath
	}
	return directoryPath + "/"
}

func newFilestoreEntryPage(entries []FilestoreEntry, limit int) FilestoreEntryPage {
	page := FilestoreEntryPage{Entries: entries, HasMore: len(entries) > limit}
	if page.HasMore {
		page.Entries = entries[:limit]
	}
	return page
}

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
	defer tx.Rollback(ctx)

	if input.Path == "/" {
		if err := tx.Commit(ctx); err != nil {
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
		directory, err = ensureFilestoreDirectoryTx(ctx, tx, input.WorkspaceID, filesystem, directoryPath, input.Actor, input.Now)
		if err != nil {
			return FilestoreEntry{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
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
	defer tx.Rollback(ctx)

	result, err := putFilestoreFileTx(ctx, tx, filesystem, putFilestoreFileTxInput{
		WorkspaceID:                input.WorkspaceID,
		Path:                       input.Path,
		Blob:                       input.Blob,
		OverwriteExisting:          input.OverwriteExisting,
		OrphanCleanupJobExternalID: input.OrphanCleanupJobExternalID,
		WorkspaceStorageLimitBytes: input.WorkspaceStorageLimitBytes,
		Actor:                      input.Actor,
		Now:                        input.Now,
	})
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
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
	defer tx.Rollback(ctx)

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
		Actor:                      input.Actor,
		Now:                        input.Now,
	})
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
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
	defer tx.Rollback(ctx)

	source, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != FilestoreEntryKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	if input.SourcePath == input.DestinationPath {
		if err := tx.Commit(ctx); err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{Entry: source}, nil
	}
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.DestinationPath)); err != nil {
		return FilestoreMutationResult{}, err
	}

	var cleanupJobs []FilestoreObjectCleanupJob
	destination, found, err := getFilestoreEntryForMutation(ctx, tx, filesystem, input.DestinationPath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if found {
		if !filestoreEntryExpired(destination, input.Now) {
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
				FilesystemExternalID: filesystem.ExternalID,
			}, destination, "move_overwrite", input.Now)
			if err != nil {
				return FilestoreMutationResult{}, err
			}
			cleanupJobs = append(cleanupJobs, job)
		}
		if _, err := tx.Exec(ctx, `
			update filestore_entries set deleted_at = $4, updated_at = $4
			where workspace_uuid = $1 and filesystem_uuid = $2 and id = $3 and deleted_at is null
		`, filesystem.WorkspaceUUID, filesystem.UUID, destination.ID, input.Now); err != nil {
			return FilestoreMutationResult{}, err
		}
		if destination.Kind == FilestoreEntryKindFile {
			if err := applyWorkspaceStorageDeltaTx(
				ctx, tx, input.WorkspaceID, 0, -filestoreInt64(destination.SizeBytes), 0,
			); err != nil {
				return FilestoreMutationResult{}, err
			}
		}
	}

	moved, err := scanFilestoreEntryPGX(tx.QueryRow(ctx, `
		update filestore_entries
		set path = $4, parent_path = $5, updated_at = $6
		where workspace_uuid = $1 and filesystem_uuid = $2 and id = $3 and deleted_at is null
		returning `+filestoreEntryColumns()+`
	`, filesystem.WorkspaceUUID, filesystem.UUID, source.ID, input.DestinationPath,
		filestoreParentPath(input.DestinationPath), input.Now))
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
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
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback(ctx)

	source, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.SourcePath)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if source.Kind != FilestoreEntryKindDirectory {
		return FilestoreMutationResult{}, ErrFilestoreNotDirectory
	}
	if input.SourcePath == input.DestinationPath {
		if err := tx.Commit(ctx); err != nil {
			return FilestoreMutationResult{}, err
		}
		return FilestoreMutationResult{Entry: source}, nil
	}
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.DestinationPath)); err != nil {
		return FilestoreMutationResult{}, err
	}
	var maxMovedPathBytes int
	// 在批量更新前按字节预演最长目标路径，避免中途触发约束而留下难以解释的错误。
	if err := tx.QueryRow(ctx, `
		select coalesce(max(
			octet_length($4::text) + octet_length(path) - octet_length($3::text)
		), 0)::int
		from filestore_entries
		where workspace_uuid = $1 and filesystem_uuid = $2 and deleted_at is null
			and (path = $3 or left(path, char_length($3) + 1) = $3 || '/')
	`, filesystem.WorkspaceUUID, filesystem.UUID, input.SourcePath, input.DestinationPath).Scan(&maxMovedPathBytes); err != nil {
		return FilestoreMutationResult{}, err
	}
	if maxMovedPathBytes > filestoreMaxPathBytes {
		return FilestoreMutationResult{}, ErrPreconditionFailed
	}
	var conflictingID int64
	err = tx.QueryRow(ctx, `
		select id
		from filestore_entries
		where workspace_uuid = $1 and filesystem_uuid = $2 and deleted_at is null
			and (expires_at is null or expires_at > now())
			and (path = $3 or left(path, char_length($3) + 1) = $3 || '/')
		limit 1
	`, filesystem.WorkspaceUUID, filesystem.UUID, input.DestinationPath).Scan(&conflictingID)
	if err == nil {
		return FilestoreMutationResult{}, ErrFilestorePathExists
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return FilestoreMutationResult{}, err
	}
	cleanupJobs, retiredBytes, err := retireExpiredFilestoreSubtreeTx(
		ctx, tx, filestoreEntryCleanupScope{
			WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
			FilesystemExternalID: filesystem.ExternalID,
		}, filesystem, input.DestinationPath, input.Now,
	)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if retiredBytes > 0 {
		if err := applyWorkspaceStorageDeltaTx(ctx, tx, input.WorkspaceID, 0, -retiredBytes, 0); err != nil {
			return FilestoreMutationResult{}, err
		}
	}

	// 利用前缀替换一次更新整棵子树；文件内容按稳定对象键寻址，无须随路径迁移。
	if _, err := tx.Exec(ctx, `
		update filestore_entries
		set path = $4 || substring(path from char_length($3) + 1),
			parent_path = case
				when path = $3 then $5
				else $4 || substring(parent_path from char_length($3) + 1)
			end,
			updated_at = $6
		where workspace_uuid = $1 and filesystem_uuid = $2 and deleted_at is null
			and (path = $3 or left(path, char_length($3) + 1) = $3 || '/')
	`, filesystem.WorkspaceUUID, filesystem.UUID, input.SourcePath, input.DestinationPath,
		filestoreParentPath(input.DestinationPath), input.Now); err != nil {
		return FilestoreMutationResult{}, err
	}
	moved, err := scanFilestoreEntryPGX(tx.QueryRow(ctx, filestoreEntrySelectSQL()+`
		where workspace_uuid = $1 and filesystem_uuid = $2 and path = $3 and deleted_at is null
	`, filesystem.WorkspaceUUID, filesystem.UUID, input.DestinationPath))
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
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
	defer tx.Rollback(ctx)
	entry, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if entry.Kind != FilestoreEntryKindFile {
		return FilestoreMutationResult{}, ErrFilestoreNotFile
	}
	job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, filestoreEntryCleanupScope{
		WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
		FilesystemExternalID: filesystem.ExternalID,
	}, entry, "remove_file", input.Now)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		update filestore_entries set deleted_at = $4, updated_at = $4
		where workspace_uuid = $1 and filesystem_uuid = $2 and id = $3 and deleted_at is null
	`, filesystem.WorkspaceUUID, filesystem.UUID, entry.ID, input.Now); err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := applyWorkspaceStorageDeltaTx(
		ctx, tx, input.WorkspaceID, 0, -filestoreInt64(entry.SizeBytes), 0,
	); err != nil {
		return FilestoreMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Entry: entry, CleanupJobs: []FilestoreObjectCleanupJob{job}}, nil
}

// RemoveFilestoreDirectory 软删除目录；递归删除时为子树内每个文件创建对象清理任务。
func (d *DB) RemoveFilestoreDirectory(ctx context.Context, input RemoveFilestoreDirectoryInput) (FilestoreMutationResult, error) {
	if err := validateFilestorePath(input.Path); err != nil {
		return FilestoreMutationResult{}, err
	}
	if input.Path == "/" {
		return FilestoreMutationResult{}, ErrFilestoreInvalidMove
	}
	input.Now = filestoreNow(input.Now)
	tx, filesystem, err := d.beginFilestoreNamespaceMutation(ctx, input.WorkspaceID, input.FilesystemID)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	defer tx.Rollback(ctx)
	entry, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if entry.Kind != FilestoreEntryKindDirectory {
		return FilestoreMutationResult{}, ErrFilestoreNotDirectory
	}

	var childCount int
	if err := tx.QueryRow(ctx, `
		select count(*)::int from filestore_entries
		where workspace_uuid = $1 and filesystem_uuid = $2 and parent_path = $3
			and deleted_at is null and (expires_at is null or expires_at > now())
	`, filesystem.WorkspaceUUID, filesystem.UUID, input.Path).Scan(&childCount); err != nil {
		return FilestoreMutationResult{}, err
	}
	if childCount > 0 && !input.Recursive {
		return FilestoreMutationResult{}, ErrFilestoreDirectoryNotEmpty
	}
	cleanupJobs, removedBytes, err := enqueueFilestoreSubtreeCleanupJobsTx(ctx, tx, filestoreEntryCleanupScope{
		WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
		FilesystemExternalID: filesystem.ExternalID,
	}, filesystem, input.Path, input.Now)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		update filestore_entries set deleted_at = $4, updated_at = $4
		where workspace_uuid = $1 and filesystem_uuid = $2 and deleted_at is null
			and (path = $3 or left(path, char_length($3) + 1) = $3 || '/')
	`, filesystem.WorkspaceUUID, filesystem.UUID, input.Path, input.Now); err != nil {
		return FilestoreMutationResult{}, err
	}
	if removedBytes > 0 {
		if err := applyWorkspaceStorageDeltaTx(ctx, tx, input.WorkspaceID, 0, -removedBytes, 0); err != nil {
			return FilestoreMutationResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return FilestoreMutationResult{}, err
	}
	return FilestoreMutationResult{Entry: entry, CleanupJobs: cleanupJobs}, nil
}

func (d *DB) beginFilestoreNamespaceMutation(ctx context.Context, workspaceID, filesystemID int64) (pgx.Tx, FilestoreFilesystem, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback(ctx)
		}
	}()
	// 即使目录操作通常不改变容量，也可能替换到期文件；统一锁序比事后升级锁更安全。
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, workspaceID); err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	filesystem, err := scanFilestoreFilesystemPGX(tx.QueryRow(ctx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = (select uuid from workspaces where id = $1)
			and id = $2 and deleted_at is null
		for update
	`, workspaceID, filesystemID))
	if err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	// 文件系统锁使用负数键，与 Files API 已占用的正数工作区锁命名空间隔离。
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(-($1::bigint))`, filesystem.ID); err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	rollback = false
	return tx, filesystem, nil
}

func (d *DB) resolveFilestoreDirectoryForRead(ctx context.Context, workspaceID, filesystemID int64, directoryPath string) (FilestoreFilesystem, error) {
	filesystem, err := getFilestoreFilesystemByIDSQLX(ctx, d.sql, workspaceID, filesystemID)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if directoryPath == "/" {
		return filesystem, nil
	}
	entry, err := getActiveFilestoreEntrySQLX(ctx, d.sql, filesystem, directoryPath)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if entry.Kind != FilestoreEntryKindDirectory {
		return FilestoreFilesystem{}, ErrFilestoreNotDirectory
	}
	return filesystem, nil
}

func requireFilestoreDirectoryTx(ctx context.Context, tx pgx.Tx, filesystem FilestoreFilesystem, directoryPath string) error {
	if directoryPath == "/" {
		return nil
	}
	entry, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, directoryPath)
	if err != nil {
		return err
	}
	if entry.Kind != FilestoreEntryKindDirectory {
		return ErrFilestoreNotDirectory
	}
	return nil
}

func getFilestoreEntryForMutation(ctx context.Context, tx pgx.Tx, filesystem FilestoreFilesystem, entryPath string) (FilestoreEntry, bool, error) {
	entry, err := scanFilestoreEntryPGX(tx.QueryRow(ctx, filestoreEntrySelectSQL()+`
		where workspace_uuid = $1 and filesystem_uuid = $2 and path = $3 and deleted_at is null
		for update
	`, filesystem.WorkspaceUUID, filesystem.UUID, entryPath))
	if errors.Is(err, ErrNotFound) {
		return FilestoreEntry{}, false, nil
	}
	return entry, err == nil, err
}

func getActiveFilestoreEntryForMutation(ctx context.Context, tx pgx.Tx, filesystem FilestoreFilesystem, entryPath string) (FilestoreEntry, error) {
	entry, err := scanFilestoreEntryPGX(tx.QueryRow(ctx, filestoreEntrySelectSQL()+`
		where workspace_uuid = $1 and filesystem_uuid = $2 and path = $3
			and deleted_at is null and (expires_at is null or expires_at > now())
		for update
	`, filesystem.WorkspaceUUID, filesystem.UUID, entryPath))
	return entry, err
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

func ensureFilestoreDirectoryTx(ctx context.Context, tx pgx.Tx, workspaceID int64, filesystem FilestoreFilesystem, directoryPath string, actor FilestoreActor, now time.Time) (FilestoreEntry, error) {
	existing, found, err := getFilestoreEntryForMutation(ctx, tx, filesystem, directoryPath)
	if err != nil {
		return FilestoreEntry{}, err
	}
	if found {
		if existing.Kind == FilestoreEntryKindDirectory {
			return existing, nil
		}
		if !filestoreEntryExpired(existing, now) {
			return FilestoreEntry{}, ErrFilestorePathExists
		}
		if _, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, filestoreEntryCleanupScope{
			WorkspaceID: workspaceID, FilesystemID: filesystem.ID,
			FilesystemExternalID: filesystem.ExternalID,
		}, existing, "expired_path_replaced", now); err != nil {
			return FilestoreEntry{}, err
		}
		directory, err := scanFilestoreEntryPGX(tx.QueryRow(ctx, `
			update filestore_entries
			set kind = 'directory', parent_path = $4,
				size_bytes = null, media_type = null, detected_mime_type = null,
				metadata = '{}'::jsonb, authorization_metadata = '{}'::jsonb,
				tags = array[]::text[], downloadable = false,
				md5 = null, sha256 = null, s3_bucket = null, s3_key = null,
				s3_etag = null, s3_version_id = null, expires_at = null,
				created_by_api_key_uuid = (select uuid from api_keys where id = $5),
				created_by_session_uuid = (select uuid from sessions where id = $6),
				created_by_code_session_uuid = (select uuid from code_sessions where id = $7),
				created_at = $8, updated_at = $8
			where workspace_uuid = $1 and filesystem_uuid = $2 and id = $3 and deleted_at is null
			returning `+filestoreEntryColumns()+`
		`, filesystem.WorkspaceUUID, filesystem.UUID, existing.ID, filestoreParentPath(directoryPath),
			filestoreNullableInt64(actor.APIKeyID), filestoreNullableInt64(actor.SessionID),
			filestoreNullableInt64(actor.CodeSessionID), now))
		if err != nil {
			return FilestoreEntry{}, err
		}
		if err := applyWorkspaceStorageDeltaTx(
			ctx, tx, workspaceID, 0, -filestoreInt64(existing.SizeBytes), 0,
		); err != nil {
			return FilestoreEntry{}, err
		}
		return directory, nil
	}
	return scanFilestoreEntryPGX(tx.QueryRow(ctx, `
		insert into filestore_entries (
			uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			kind, path, parent_path,
			created_by_api_key_uuid, created_by_session_uuid, created_by_code_session_uuid,
			created_at, updated_at
		)
		values (
			gen_random_uuid(), concat('fse_', replace(gen_random_uuid()::text, '-', '')),
			$1, $2, $3, 'directory', $4, $5,
			(select uuid from api_keys where id = $6),
			(select uuid from sessions where id = $7),
			(select uuid from code_sessions where id = $8),
			$9, $9
		)
		returning `+filestoreEntryColumns()+`
	`, filesystem.OrganizationUUID, filesystem.WorkspaceUUID, filesystem.UUID,
		directoryPath, filestoreParentPath(directoryPath), filestoreNullableInt64(actor.APIKeyID),
		filestoreNullableInt64(actor.SessionID), filestoreNullableInt64(actor.CodeSessionID), now))
}

type putFilestoreFileTxInput struct {
	WorkspaceID                int64
	Path                       string
	Blob                       FilestoreFileBlob
	OverwriteExisting          bool
	OrphanCleanupJobExternalID string
	WorkspaceStorageLimitBytes int64
	Actor                      FilestoreActor
	Now                        time.Time
}

func putFilestoreFileTx(ctx context.Context, tx pgx.Tx, filesystem FilestoreFilesystem, input putFilestoreFileTxInput) (FilestoreMutationResult, error) {
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.Path)); err != nil {
		return FilestoreMutationResult{}, err
	}
	existing, found, err := getFilestoreEntryForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	var quotaNow time.Time
	// 以数据库时钟判定过期，保证配额查询与当前事务看到同一时间基准。
	if err := tx.QueryRow(ctx, `select now()`).Scan(&quotaNow); err != nil {
		return FilestoreMutationResult{}, err
	}
	var oldSize int64
	if found && existing.Kind == FilestoreEntryKindFile {
		// 账本在 TTL 清理提交前仍统计到期文件；复用路径时必须以完整旧大小计算增量。
		oldSize = filestoreInt64(existing.SizeBytes)
	}
	if found && !filestoreEntryExpired(existing, quotaNow) {
		if existing.Kind != FilestoreEntryKindFile {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
		if !input.OverwriteExisting {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
	}
	storageDelta := input.Blob.SizeBytes - oldSize
	if err := applyWorkspaceStorageDeltaTx(
		ctx, tx, input.WorkspaceID, 0, storageDelta, input.WorkspaceStorageLimitBytes,
	); err != nil {
		return FilestoreMutationResult{}, err
	}

	var cleanupJobs []FilestoreObjectCleanupJob
	if found && existing.Kind == FilestoreEntryKindFile && !sameFilestoreObject(existing, input.Blob) {
		job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, filestoreEntryCleanupScope{
			WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
			FilesystemExternalID: filesystem.ExternalID,
		}, existing, "file_replaced", input.Now)
		if err != nil {
			return FilestoreMutationResult{}, err
		}
		cleanupJobs = append(cleanupJobs, job)
	}
	entry, err := writeFilestoreFileTx(ctx, tx, filesystem, existing, found, input)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	if input.OrphanCleanupJobExternalID != "" {
		// 只有桶、键、版本均与待落库对象吻合时才取消哨兵，防止错绑任务掩盖孤儿。
		if err := cancelAttachedFilestoreObjectCleanupJobTx(ctx, tx, input.WorkspaceID, input.OrphanCleanupJobExternalID, input.Blob); err != nil {
			return FilestoreMutationResult{}, err
		}
	}
	return FilestoreMutationResult{Entry: entry, CleanupJobs: cleanupJobs}, nil
}

func writeFilestoreFileTx(ctx context.Context, tx pgx.Tx, filesystem FilestoreFilesystem, existing FilestoreEntry, found bool, input putFilestoreFileTxInput) (FilestoreEntry, error) {
	metadata := filestoreJSONObject(input.Blob.Metadata)
	authorizationMetadata := filestoreJSONObject(input.Blob.AuthorizationMetadata)
	tags := filestoreTags(input.Blob.Tags)
	args := []any{
		input.Path, filestoreParentPath(input.Path), input.Blob.SizeBytes, input.Blob.MediaType,
		filestoreNullableString(input.Blob.DetectedMimeType), metadata, authorizationMetadata,
		tags, input.Blob.Downloadable, input.Blob.MD5, input.Blob.SHA256,
		input.Blob.S3Bucket, input.Blob.S3Key, filestoreNullableString(input.Blob.S3ETag),
		filestoreNullableString(input.Blob.S3VersionID), input.Blob.ExpiresAt,
		filestoreNullableInt64(input.Actor.APIKeyID), filestoreNullableInt64(input.Actor.SessionID),
		filestoreNullableInt64(input.Actor.CodeSessionID), input.Now,
	}
	if found {
		return scanFilestoreEntryPGX(tx.QueryRow(ctx, `
			update filestore_entries
			set kind = 'file', path = $4, parent_path = $5,
				size_bytes = $6, media_type = $7, detected_mime_type = $8,
				metadata = $9::jsonb, authorization_metadata = $10::jsonb, tags = $11,
				downloadable = $12, md5 = $13, sha256 = $14, s3_bucket = $15,
				s3_key = $16, s3_etag = $17, s3_version_id = $18, expires_at = $19,
				created_by_api_key_uuid = (select uuid from api_keys where id = $20),
				created_by_session_uuid = (select uuid from sessions where id = $21),
				created_by_code_session_uuid = (select uuid from code_sessions where id = $22),
				created_at = $23, updated_at = $23
			where workspace_uuid = $1 and filesystem_uuid = $2 and id = $3 and deleted_at is null
			returning `+filestoreEntryColumns()+`
		`, append([]any{filesystem.WorkspaceUUID, filesystem.UUID, existing.ID}, args...)...))
	}
	return scanFilestoreEntryPGX(tx.QueryRow(ctx, `
		insert into filestore_entries (
			uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			kind, path, parent_path, size_bytes, media_type, detected_mime_type,
			metadata, authorization_metadata, tags, downloadable, md5, sha256,
			s3_bucket, s3_key, s3_etag, s3_version_id, expires_at,
			created_by_api_key_uuid, created_by_session_uuid, created_by_code_session_uuid,
			created_at, updated_at
		)
		values (
			gen_random_uuid(), concat('fse_', replace(gen_random_uuid()::text, '-', '')),
			$1, $2, $3, 'file', $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb,
			$11, $12, $13, $14, $15, $16, $17, $18, $19,
			(select uuid from api_keys where id = $20),
			(select uuid from sessions where id = $21),
			(select uuid from code_sessions where id = $22),
			$23, $23
		)
		returning `+filestoreEntryColumns()+`
	`, filesystem.OrganizationUUID, filesystem.WorkspaceUUID, filesystem.UUID,
		input.Path, filestoreParentPath(input.Path), input.Blob.SizeBytes, input.Blob.MediaType,
		filestoreNullableString(input.Blob.DetectedMimeType), metadata, authorizationMetadata,
		tags, input.Blob.Downloadable, input.Blob.MD5, input.Blob.SHA256,
		input.Blob.S3Bucket, input.Blob.S3Key, filestoreNullableString(input.Blob.S3ETag),
		filestoreNullableString(input.Blob.S3VersionID), input.Blob.ExpiresAt,
		filestoreNullableInt64(input.Actor.APIKeyID), filestoreNullableInt64(input.Actor.SessionID),
		filestoreNullableInt64(input.Actor.CodeSessionID), input.Now))
}

func filestoreBlobFromEntry(entry FilestoreEntry) FilestoreFileBlob {
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

func filestoreEntryExpired(entry FilestoreEntry, now time.Time) bool {
	return entry.ExpiresAt != nil && !entry.ExpiresAt.After(now)
}

func sameFilestoreObject(entry FilestoreEntry, blob FilestoreFileBlob) bool {
	return filestoreString(entry.S3Bucket) == blob.S3Bucket && filestoreString(entry.S3Key) == blob.S3Key && filestoreString(entry.S3VersionID) == blob.S3VersionID
}
