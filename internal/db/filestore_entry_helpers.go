package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/filestorepath"

	"github.com/jmoiron/sqlx"
)

func (d *DB) beginFilestoreNamespaceMutation(ctx context.Context, workspaceID, filesystemID int64) (*sqlx.Tx, FilestoreFilesystem, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	rollback := true
	defer func() {
		if rollback {
			_ = tx.Rollback()
		}
	}()
	// 即使目录操作通常不改变容量，也可能替换到期文件；统一锁序比事后升级锁更安全。
	if _, err := namedExecContext(ctx, tx, `
		select pg_advisory_xact_lock(:workspace_id)
	`, map[string]any{"workspace_id": workspaceID}); err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = (select uuid from workspaces where id = :workspace_id)
			and id = :filesystem_id and deleted_at is null
		for update
	`, map[string]any{
		"workspace_id":  workspaceID,
		"filesystem_id": filesystemID,
	})
	if err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	// 文件系统锁使用负数键，与 Files API 已占用的正数工作区锁命名空间隔离。
	if _, err := namedExecContext(ctx, tx, `
		select pg_advisory_xact_lock(-CAST(:filesystem_id AS bigint))
	`, map[string]any{"filesystem_id": filesystem.ID}); err != nil {
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

func requireFilestoreDirectoryTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, directoryPath string) error {
	if directoryPath == "/" {
		return nil
	}
	entry, err := getActiveFilestoreEntryForMutation(ctx, tx, filesystem, directoryPath)
	if errors.Is(err, ErrNotFound) {
		return ErrFilestoreParentMissing
	}
	if err != nil {
		return err
	}
	if entry.Kind != FilestoreEntryKindDirectory {
		return ErrFilestoreNotDirectory
	}
	return nil
}

func getFilestoreEntryForMutation(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, entryPath string) (FilestoreEntry, bool, error) {
	entry, err := getFilestoreEntrySQLX(ctx, tx, filestoreEntrySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and path = :entry_path
			and deleted_at is null
		for update
	`, filestoreEntryMutationArguments(filesystem, entryPath))
	if errors.Is(err, ErrNotFound) {
		return FilestoreEntry{}, false, nil
	}
	return entry, err == nil, err
}

func getActiveFilestoreEntryForMutation(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, entryPath string) (FilestoreEntry, error) {
	return getFilestoreEntrySQLX(ctx, tx, filestoreEntrySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and path = :entry_path
			and deleted_at is null and (expires_at is null or expires_at > now())
		for update
	`, filestoreEntryMutationArguments(filesystem, entryPath))
}

func filestoreSubtreeContainsManagedEntryTx(
	ctx context.Context,
	tx *sqlx.Tx,
	filesystem FilestoreFilesystem,
	rootPath string,
) (bool, error) {
	var containsManagedEntry bool
	err := namedGetContext(ctx, tx, &containsManagedEntry, `
		select exists (
			select 1
			from filestore_entries
			where workspace_uuid = :workspace_uuid
				and filesystem_uuid = :filesystem_uuid
				and deleted_at is null
				and (
					path = :root_path
					or left(path, char_length(:root_path) + 1) = :root_path || '/'
				)
				and (
					managed_by is not null
					or managed_resource_external_id is not null
					or source_file_uuid is not null
				)
		)
	`, map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"root_path":       rootPath,
	})
	return containsManagedEntry, err
}

func filestoreEntryMutationArguments(filesystem FilestoreFilesystem, entryPath string) map[string]any {
	return map[string]any{
		"workspace_uuid":  filesystem.WorkspaceUUID,
		"filesystem_uuid": filesystem.UUID,
		"entry_path":      entryPath,
	}
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

func ensureFilestoreDirectoryTx(ctx context.Context, tx *sqlx.Tx, workspaceID int64, filesystem FilestoreFilesystem, directoryPath string, now time.Time) (FilestoreEntry, error) {
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
		}, existing, "expired_path_replaced", now); err != nil {
			return FilestoreEntry{}, err
		}
		directory, err := getFilestoreEntrySQLX(ctx, tx, `
			update filestore_entries
			set kind = 'directory', parent_path = :parent_path,
				size_bytes = null, media_type = null, detected_mime_type = null,
				metadata = CAST('{}' AS jsonb), authorization_metadata = CAST('{}' AS jsonb),
				tags = CAST(array[] AS text[]), downloadable = false,
				md5 = null, sha256 = null, s3_bucket = null, s3_key = null,
				s3_etag = null, s3_version_id = null, expires_at = null,
				managed_by = null, managed_resource_external_id = null,
				source_file_uuid = null,
				created_by_api_key_uuid = :created_by_api_key_uuid,
				created_by_session_uuid = :created_by_session_uuid,
				created_by_code_session_uuid = :created_by_code_session_uuid,
				created_at = :now, updated_at = :now
			where workspace_uuid = :workspace_uuid
				and filesystem_uuid = :filesystem_uuid
				and id = :entry_id
				and deleted_at is null
			returning `+filestoreEntryColumns()+`
		`, map[string]any{
			"workspace_uuid":               filesystem.WorkspaceUUID,
			"filesystem_uuid":              filesystem.UUID,
			"entry_id":                     existing.ID,
			"parent_path":                  filestoreParentPath(directoryPath),
			"created_by_api_key_uuid":      filesystem.CreatedByAPIKeyUUID,
			"created_by_session_uuid":      filesystem.SessionUUID,
			"created_by_code_session_uuid": filesystem.CodeSessionUUID,
			"now":                          now,
		})
		if err != nil {
			return FilestoreEntry{}, err
		}
		if err := applyWorkspaceStorageDeltaSQLXTx(
			ctx, tx, workspaceID, 0, -filestoreInt64(existing.SizeBytes), 0,
		); err != nil {
			return FilestoreEntry{}, err
		}
		return directory, nil
	}
	return getFilestoreEntrySQLX(ctx, tx, `
		insert into filestore_entries (
			uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			kind, path, parent_path,
			created_by_api_key_uuid, created_by_session_uuid, created_by_code_session_uuid,
			created_at, updated_at
		)
		values (
			gen_random_uuid(), concat('fse_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:organization_uuid, :workspace_uuid, :filesystem_uuid,
			'directory', :entry_path, :parent_path,
			:created_by_api_key_uuid, :created_by_session_uuid, :created_by_code_session_uuid,
			:now, :now
		)
		returning `+filestoreEntryColumns()+`
	`, map[string]any{
		"organization_uuid":            filesystem.OrganizationUUID,
		"workspace_uuid":               filesystem.WorkspaceUUID,
		"filesystem_uuid":              filesystem.UUID,
		"entry_path":                   directoryPath,
		"parent_path":                  filestoreParentPath(directoryPath),
		"created_by_api_key_uuid":      filesystem.CreatedByAPIKeyUUID,
		"created_by_session_uuid":      filesystem.SessionUUID,
		"created_by_code_session_uuid": filesystem.CodeSessionUUID,
		"now":                          now,
	})
}

type putFilestoreFileTxInput struct {
	WorkspaceID                int64
	Path                       string
	Blob                       FilestoreFileBlob
	OverwriteExisting          bool
	OrphanCleanupJobExternalID string
	WorkspaceStorageLimitBytes int64
	Now                        time.Time
}

func putFilestoreFileTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, input putFilestoreFileTxInput) (FilestoreMutationResult, error) {
	if err := requireFilestoreDirectoryTx(ctx, tx, filesystem, filestoreParentPath(input.Path)); err != nil {
		return FilestoreMutationResult{}, err
	}
	existing, found, err := getFilestoreEntryForMutation(ctx, tx, filesystem, input.Path)
	if err != nil {
		return FilestoreMutationResult{}, err
	}
	var quotaNow time.Time
	// 以数据库时钟判定过期，保证配额查询与当前事务看到同一时间基准。
	if err := tx.GetContext(ctx, &quotaNow, `select now()`); err != nil {
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
		if existing.SourceFileUUID != nil {
			return FilestoreMutationResult{}, ErrPreconditionFailed
		}
		if !input.OverwriteExisting {
			return FilestoreMutationResult{}, ErrFilestorePathExists
		}
	}
	storageDelta := input.Blob.SizeBytes - oldSize
	if err := applyWorkspaceStorageDeltaSQLXTx(
		ctx, tx, input.WorkspaceID, 0, storageDelta, input.WorkspaceStorageLimitBytes,
	); err != nil {
		return FilestoreMutationResult{}, err
	}

	var cleanupJobs []FilestoreObjectCleanupJob
	if found && existing.Kind == FilestoreEntryKindFile && !sameFilestoreObject(existing, input.Blob) {
		job, err := enqueueFilestoreEntryCleanupJobTx(ctx, tx, filestoreEntryCleanupScope{
			WorkspaceID: input.WorkspaceID, FilesystemID: filesystem.ID,
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

func writeFilestoreFileTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, existing FilestoreEntry, found bool, input putFilestoreFileTxInput) (FilestoreEntry, error) {
	arguments := filestoreFileWriteArguments(filesystem, input)
	if found {
		arguments["entry_id"] = existing.ID
		return getFilestoreEntrySQLX(ctx, tx, `
			update filestore_entries
			set kind = 'file', path = :entry_path, parent_path = :parent_path,
				size_bytes = :size_bytes, media_type = :media_type,
				detected_mime_type = :detected_mime_type,
				metadata = CAST(:metadata AS jsonb),
				authorization_metadata = CAST(:authorization_metadata AS jsonb),
				tags = :tags, downloadable = :downloadable, md5 = :md5, sha256 = :sha256,
				s3_bucket = :s3_bucket, s3_key = :s3_key, s3_etag = :s3_etag,
				s3_version_id = :s3_version_id, expires_at = :expires_at,
				managed_by = null, managed_resource_external_id = null,
				source_file_uuid = null,
				created_by_api_key_uuid = :created_by_api_key_uuid,
				created_by_session_uuid = :created_by_session_uuid,
				created_by_code_session_uuid = :created_by_code_session_uuid,
				created_at = :now, updated_at = :now
			where workspace_uuid = :workspace_uuid
				and filesystem_uuid = :filesystem_uuid
				and id = :entry_id
				and deleted_at is null
			returning `+filestoreEntryColumns()+`
		`, arguments)
	}
	return getFilestoreEntrySQLX(ctx, tx, `
		insert into filestore_entries (
			uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			kind, path, parent_path, size_bytes, media_type, detected_mime_type,
			metadata, authorization_metadata, tags, downloadable, md5, sha256,
			s3_bucket, s3_key, s3_etag, s3_version_id, expires_at,
			created_by_api_key_uuid, created_by_session_uuid, created_by_code_session_uuid,
			created_at, updated_at
		)
		values (
			gen_random_uuid(), concat('fse_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:organization_uuid, :workspace_uuid, :filesystem_uuid,
			'file', :entry_path, :parent_path, :size_bytes, :media_type,
			:detected_mime_type, CAST(:metadata AS jsonb),
			CAST(:authorization_metadata AS jsonb), :tags, :downloadable, :md5, :sha256,
			:s3_bucket, :s3_key, :s3_etag, :s3_version_id, :expires_at,
			:created_by_api_key_uuid, :created_by_session_uuid, :created_by_code_session_uuid,
			:now, :now
		)
		returning `+filestoreEntryColumns()+`
	`, arguments)
}

func filestoreFileWriteArguments(filesystem FilestoreFilesystem, input putFilestoreFileTxInput) map[string]any {
	return map[string]any{
		"organization_uuid":            filesystem.OrganizationUUID,
		"workspace_uuid":               filesystem.WorkspaceUUID,
		"filesystem_uuid":              filesystem.UUID,
		"entry_path":                   input.Path,
		"parent_path":                  filestoreParentPath(input.Path),
		"size_bytes":                   input.Blob.SizeBytes,
		"media_type":                   input.Blob.MediaType,
		"detected_mime_type":           filestoreNullableString(input.Blob.DetectedMimeType),
		"metadata":                     string(filestoreJSONObject(input.Blob.Metadata)),
		"authorization_metadata":       string(filestoreJSONObject(input.Blob.AuthorizationMetadata)),
		"tags":                         filestoreTags(input.Blob.Tags),
		"downloadable":                 input.Blob.Downloadable,
		"md5":                          input.Blob.MD5,
		"sha256":                       input.Blob.SHA256,
		"s3_bucket":                    input.Blob.S3Bucket,
		"s3_key":                       input.Blob.S3Key,
		"s3_etag":                      filestoreNullableString(input.Blob.S3ETag),
		"s3_version_id":                filestoreNullableString(input.Blob.S3VersionID),
		"expires_at":                   input.Blob.ExpiresAt,
		"created_by_api_key_uuid":      filesystem.CreatedByAPIKeyUUID,
		"created_by_session_uuid":      filesystem.SessionUUID,
		"created_by_code_session_uuid": filesystem.CodeSessionUUID,
		"now":                          input.Now,
	}
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
