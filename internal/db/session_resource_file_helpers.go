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
	"github.com/jmoiron/sqlx"
)

func (d *DB) beginFilestoreNamespaceMutation(ctx context.Context, workspaceUUID, filesystemUUID string) (*sqlx.Tx, FilestoreFilesystem, error) {
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
	if _, err := namedExecContext(ctx, tx, fileWorkspaceLockQuery, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
	}); err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and uuid = :filesystem_uuid and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid":  dbUUID(workspaceUUID),
		"filesystem_uuid": dbUUID(filesystemUUID),
	})
	if err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	if _, err := namedExecContext(ctx, tx, provisionFilestoreNamespaceLockQuery, map[string]any{
		"filesystem_uuid": dbUUID(filesystem.UUID),
	}); err != nil {
		return nil, FilestoreFilesystem{}, err
	}
	rollback = false
	return tx, filesystem, nil
}

func (d *DB) resolveFilestoreDirectoryForRead(ctx context.Context, workspaceUUID, filesystemUUID string, directoryPath string) (FilestoreFilesystem, error) {
	filesystem, err := getFilestoreFilesystemByUUIDSQLX(ctx, d.sql, workspaceUUID, filesystemUUID)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if directoryPath == "/" {
		return filesystem, nil
	}
	entry, err := getActiveSessionResourceFileSQLX(ctx, d.sql, filesystem, directoryPath)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if entry.Kind != SessionResourceFileKindDirectory {
		return FilestoreFilesystem{}, ErrFilestoreNotDirectory
	}
	return filesystem, nil
}

func requireFilestoreDirectoryTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, directoryPath string) error {
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

func getSessionResourceFileForMutation(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, entryPath string) (SessionResourceFile, bool, error) {
	entry, err := getSessionResourceFileSQLX(ctx, tx, sessionResourceFileSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and path = :entry_path
			and deleted_at is null
	`, sessionResourceFileMutationArguments(filesystem, entryPath))
	if errors.Is(err, ErrNotFound) {
		return SessionResourceFile{}, false, nil
	}
	return entry, err == nil, err
}

func getActiveSessionResourceFileForMutation(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, entryPath string) (SessionResourceFile, error) {
	return getSessionResourceFileSQLX(ctx, tx, sessionResourceFileSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and path = :entry_path
			and deleted_at is null and (expires_at is null or expires_at > now())
	`, sessionResourceFileMutationArguments(filesystem, entryPath))
}

func sessionResourceFileMutationArguments(filesystem FilestoreFilesystem, entryPath string) map[string]any {
	return map[string]any{
		"workspace_uuid": dbUUID(filesystem.WorkspaceUUID),
		"session_uuid":   dbUUID(filesystem.SessionUUID),
		"entry_path":     entryPath,
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

// 如果这个 directoryPath 已经存在，而且本来就是目录，就直接返回现有目录。
// 如果这个路径被非目录 entry 占着，就返回 ErrFilestorePathExists。
// 如果这个路径根本不存在，就插入一条新的 directory entry。
func ensureFilestoreDirectoryTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, directoryPath string, now time.Time) (SessionResourceFile, error) {
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
	var insertedID int64
	err = namedGetContext(ctx, tx, &insertedID, `
		insert into session_resources (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, resource_type, payload, secret_payload,
			path, parent_path, created_at, updated_at
		)
		select :resource_uuid,
			:resource_external_id,
			:organization_uuid, :workspace_uuid, session.uuid,
			session.external_id, 'directory', null, null,
			:entry_path, :parent_path, :now, :now
		from sessions session
		where session.uuid = :session_uuid
			and session.workspace_uuid = :workspace_uuid
			and session.deleted_at is null
		returning id
	`, map[string]any{
		"resource_uuid":        dbUUID(resourceUUID),
		"resource_external_id": resourceExternalID,
		"organization_uuid":    dbUUID(filesystem.OrganizationUUID),
		"workspace_uuid":       dbUUID(filesystem.WorkspaceUUID),
		"session_uuid":         dbUUID(filesystem.SessionUUID),
		"entry_path":           directoryPath,
		"parent_path":          filestoreParentPath(directoryPath),
		"now":                  now,
	})
	if err := mapSessionNamespaceInsertError(err); err != nil {
		return SessionResourceFile{}, err
	}
	return getSessionResourceFileSQLX(ctx, tx, sessionResourceFileSelectSQL()+`
		where uuid = :resource_uuid and deleted_at is null
	`, map[string]any{"resource_uuid": dbUUID(resourceUUID)})
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

func putFilestoreFileTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, input putFilestoreFileTxInput) (FilestoreMutationResult, error) {
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
	if err := applyWorkspaceStorageDeltaSQLXTx(
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

func writeFilestoreFileTx(ctx context.Context, tx *sqlx.Tx, filesystem FilestoreFilesystem, existing SessionResourceFile, found bool, input putFilestoreFileTxInput) (SessionResourceFile, error) {
	arguments := filestoreFileWriteArguments(filesystem, input)
	if found {
		arguments["resource_uuid"] = dbUUID(existing.UUID)
		if _, err := namedExecContext(ctx, tx, `
			update files file
			set filename = :filename, mime_type = :media_type,
				detected_mime_type = :detected_mime_type, size_bytes = :size_bytes,
				metadata = CAST(:metadata AS jsonb),
				authorization_metadata = CAST(:authorization_metadata AS jsonb),
				tags = :tags, downloadable = :downloadable, md5 = :md5,
				sha256 = :sha256, s3_bucket = :s3_bucket, s3_key = :s3_key,
				s3_etag = :s3_etag, s3_version_id = :s3_version_id,
				deleted_at = null
			where file.workspace_uuid = :workspace_uuid
				and file.uuid = (
				select resource.file_uuid from session_resources resource
				where resource.uuid = :resource_uuid
					and resource.workspace_uuid = :workspace_uuid
					and resource.session_uuid = :session_uuid
			)
		`, arguments); err != nil {
			return SessionResourceFile{}, err
		}
		if _, err := namedExecContext(ctx, tx, `
			update session_resources
			set resource_type = 'file', payload = null, secret_payload = null,
				path = :entry_path, parent_path = :parent_path,
				expires_at = :expires_at, updated_at = :now
			where uuid = :resource_uuid
				and workspace_uuid = :workspace_uuid
				and session_uuid = :session_uuid and deleted_at is null
		`, arguments); err != nil {
			return SessionResourceFile{}, err
		}
		return getSessionResourceFileSQLX(ctx, tx, sessionResourceFileSelectSQL()+`
			where uuid = :resource_uuid and deleted_at is null
		`, arguments)
	}
	fileUUID, fileExternalID, err := newFileIdentity()
	if err != nil {
		return SessionResourceFile{}, err
	}
	resourceUUID, resourceExternalID, err := newSessionResourceIdentity()
	if err != nil {
		return SessionResourceFile{}, err
	}
	arguments["file_uuid"] = dbUUID(fileUUID)
	arguments["file_external_id"] = fileExternalID
	arguments["resource_uuid"] = dbUUID(resourceUUID)
	arguments["resource_external_id"] = resourceExternalID
	var insertedID int64
	err = namedGetContext(ctx, tx, &insertedID, `
		with inserted_file as (
			insert into files (
				uuid, external_id, workspace_uuid, filename, mime_type,
				detected_mime_type, size_bytes, metadata, authorization_metadata,
				tags, downloadable, md5, sha256, s3_bucket, s3_key,
				s3_etag, s3_version_id, scope_type, scope_id,
				created_by_api_key_uuid, created_at
			)
			select :file_uuid,
				:file_external_id,
				:workspace_uuid, :filename, :media_type, :detected_mime_type,
				:size_bytes, CAST(:metadata AS jsonb),
				CAST(:authorization_metadata AS jsonb), :tags, :downloadable,
				:md5, :sha256, :s3_bucket, :s3_key, :s3_etag, :s3_version_id,
				'session', session.external_id,
				coalesce(api_key.uuid, session.created_by_api_key_uuid), :now
			from sessions session
			left join api_keys api_key
				on api_key.uuid = :created_by_api_key_uuid
				and api_key.workspace_uuid = session.workspace_uuid
			where session.uuid = :session_uuid
				and session.workspace_uuid = :workspace_uuid
				and session.deleted_at is null
			returning uuid
		)
		insert into session_resources (
			uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
			session_external_id, resource_type, payload, secret_payload,
			path, parent_path, file_uuid, expires_at, created_at, updated_at
		)
		select :resource_uuid,
			:resource_external_id,
			:organization_uuid, :workspace_uuid, session.uuid,
			session.external_id, 'file', null, null, :entry_path, :parent_path,
			inserted_file.uuid, :expires_at, :now, :now
		from inserted_file
		join sessions session on session.uuid = :session_uuid
		returning id
	`, arguments)
	if err := mapSessionNamespaceInsertError(err); err != nil {
		return SessionResourceFile{}, err
	}
	arguments["resource_uuid"] = dbUUID(resourceUUID)
	return getSessionResourceFileSQLX(ctx, tx, sessionResourceFileSelectSQL()+`
		where uuid = :resource_uuid and deleted_at is null
	`, arguments)
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

func filestoreFileWriteArguments(filesystem FilestoreFilesystem, input putFilestoreFileTxInput) map[string]any {
	return map[string]any{
		"organization_uuid":       dbUUID(filesystem.OrganizationUUID),
		"workspace_uuid":          dbUUID(filesystem.WorkspaceUUID),
		"entry_path":              input.Path,
		"filename":                path.Base(input.Path),
		"parent_path":             filestoreParentPath(input.Path),
		"size_bytes":              input.Blob.SizeBytes,
		"media_type":              input.Blob.MediaType,
		"detected_mime_type":      filestoreNullableString(input.Blob.DetectedMimeType),
		"metadata":                string(filestoreJSONObject(input.Blob.Metadata)),
		"authorization_metadata":  string(filestoreJSONObject(input.Blob.AuthorizationMetadata)),
		"tags":                    filestoreTags(input.Blob.Tags),
		"downloadable":            input.Blob.Downloadable,
		"md5":                     input.Blob.MD5,
		"sha256":                  input.Blob.SHA256,
		"s3_bucket":               input.Blob.S3Bucket,
		"s3_key":                  input.Blob.S3Key,
		"s3_etag":                 filestoreNullableString(input.Blob.S3ETag),
		"s3_version_id":           filestoreNullableString(input.Blob.S3VersionID),
		"expires_at":              input.Blob.ExpiresAt,
		"created_by_api_key_uuid": dbNullableUUID(filesystem.CreatedByAPIKeyUUID),
		"session_uuid":            dbUUID(filesystem.SessionUUID),

		"now": input.Now,
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
	tx *sqlx.Tx,
	workspaceUUID string,
	resourceUUID string,
	retiredAt time.Time,
) error {
	if _, err := namedExecContext(ctx, tx, `
		update files file
		set deleted_at = coalesce(file.deleted_at, :retired_at)
		where file.workspace_uuid = :workspace_uuid
			and file.uuid = (
				select resource.file_uuid
				from session_resources resource
				where resource.uuid = :resource_uuid
					and resource.workspace_uuid = :workspace_uuid
					and resource.payload is null
			)
	`, map[string]any{
		"resource_uuid":  dbUUID(resourceUUID),
		"workspace_uuid": dbUUID(workspaceUUID),
		"retired_at":     retiredAt,
	}); err != nil {
		return err
	}
	_, err := namedExecContext(ctx, tx, `
		update session_resources
		set deleted_at = coalesce(deleted_at, :retired_at), updated_at = :retired_at
		where uuid = :resource_uuid
			and workspace_uuid = :workspace_uuid
			and deleted_at is null
	`, map[string]any{
		"resource_uuid":  dbUUID(resourceUUID),
		"workspace_uuid": dbUUID(workspaceUUID),
		"retired_at":     retiredAt,
	})
	return err
}
