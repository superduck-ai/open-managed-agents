package db

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"time"

	"github.com/jmoiron/sqlx"
)

const (
	sessionFileResourceManagedBy = "session_file_resource"
	sessionFileProjectionScope   = "session"
	sessionOutputsRootPath       = "/outputs"
	sessionUploadsRootPath       = "/uploads"
	countSessionFileResourcesSQL = `
		select count(*)
		from session_resources
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and resource_type = :resource_type
			and deleted_at is null
	`
	findSessionFileMountConflictSQL = `
		select entry.path
		from filestore_entries entry
		cross join (values (CAST(:entry_path AS text))) as candidate(path)
		where entry.workspace_uuid = :workspace_uuid
			and entry.filesystem_uuid = :filesystem_uuid
			and entry.managed_by = :managed_by
			and entry.source_file_uuid is not null
			and entry.deleted_at is null
			and (
				entry.path = candidate.path
				or left(entry.path, length(candidate.path) + 1) = concat(candidate.path, '/')
				or left(candidate.path, length(entry.path) + 1) = concat(entry.path, '/')
			)
		order by entry.path
		limit 1
	`
	upsertSessionFileProjectionSQL = `
		insert into files (
			uuid, external_id, workspace_uuid, filename, mime_type, size_bytes, sha256,
			s3_bucket, s3_key, downloadable, scope_type, scope_id,
			created_by_api_key_uuid, created_at
		)
		values (
			:file_uuid,
			concat('file_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:workspace_uuid, :filename, :mime_type, :size_bytes, :sha256,
			:s3_bucket, :s3_key, :downloadable, :scope_type, :scope_id,
			:created_by_api_key_uuid, :created_at
		)
		on conflict (uuid) do update
		set filename = excluded.filename,
			mime_type = excluded.mime_type,
			size_bytes = excluded.size_bytes,
			sha256 = excluded.sha256,
			s3_bucket = excluded.s3_bucket,
			s3_key = excluded.s3_key,
			downloadable = excluded.downloadable,
			scope_type = excluded.scope_type,
			scope_id = excluded.scope_id,
			created_by_api_key_uuid = excluded.created_by_api_key_uuid,
			created_at = excluded.created_at,
			deleted_at = null
	`
	softDeleteSessionFileProjectionSQL = `
		update files
		set deleted_at = now()
		where workspace_uuid = :workspace_uuid
			and uuid = :file_uuid
			and scope_type = :scope_type
			and scope_id = :scope_id
			and deleted_at is null
	`
	softDeleteSessionFileProjectionsByScopeSQL = `
		update files
		set deleted_at = now()
		where workspace_uuid = :workspace_uuid
			and scope_type = :scope_type
			and scope_id = :scope_id
			and deleted_at is null
	`
	softDeleteSessionFileProjectionByEntrySQL = `
		update files projection
		set deleted_at = now()
		where projection.workspace_uuid = :workspace_uuid
			and projection.scope_type = :scope_type
			and projection.uuid = :file_uuid
			and projection.deleted_at is null
	`
	softDeleteSessionFileProjectionSubtreeSQL = `
		update files projection
		set deleted_at = now()
		where projection.workspace_uuid = :workspace_uuid
			and projection.scope_type = :scope_type
			and projection.deleted_at is null
			and exists (
				select 1
				from filestore_entries entry
				where entry.workspace_uuid = :workspace_uuid
					and entry.filesystem_uuid = :filesystem_uuid
					and entry.uuid = projection.uuid
					and (
						entry.path = :root_path
						or left(entry.path, char_length(:root_path) + 1) = :root_path || '/'
					)
			)
	`
)

func enforceSessionFileResourceCapacityTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	sessionExternalID string,
	additionalFiles int,
) error {
	// Resource mutations hold the owning Session row lock. Session creation
	// creates that row in the same transaction before checking capacity.
	if additionalFiles == 0 {
		return nil
	}
	var activeFiles int
	if err := namedGetContext(ctx, tx, &activeFiles, countSessionFileResourcesSQL, map[string]any{
		"workspace_uuid":      dbUUID(workspaceUUID),
		"session_external_id": sessionExternalID,
		"resource_type":       SessionResourceTypeFile,
	}); err != nil {
		return err
	}
	if activeFiles+additionalFiles > MaxSessionFileResources {
		return &SessionFileResourceLimitError{Limit: MaxSessionFileResources}
	}
	return nil
}

func sessionFileResourceCount(resources []CreateSessionResourceInput) int {
	count := 0
	for _, input := range resources {
		if input.Resource.ResourceType == SessionResourceTypeFile {
			count++
		}
	}
	return count
}

func sessionHasFileMount(resources []CreateSessionResourceInput) bool {
	for _, input := range resources {
		if input.FileMount != nil {
			return true
		}
	}
	return false
}

func lockSessionFilestoreMutationTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
) (FilestoreFilesystem, error) {
	if _, err := namedExecContext(ctx, tx, fileWorkspaceLockQuery, map[string]any{
		"workspace_uuid": dbUUID(session.WorkspaceUUID),
	}); err != nil {
		return FilestoreFilesystem{}, err
	}
	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and session_uuid = :session_uuid
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid": dbUUID(session.WorkspaceUUID),
		"session_uuid":   dbUUID(session.UUID),
	})
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		select pg_advisory_xact_lock(
			hashtextextended(
				concat('filestore-filesystem', chr(58), CAST(:filesystem_uuid AS text)),
				0
			)
		)
	`, map[string]any{"filesystem_uuid": dbUUID(filesystem.UUID)}); err != nil {
		return FilestoreFilesystem{}, err
	}
	return filesystem, nil
}

func bindSessionFileResourceWithLockedFilesystemTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	filesystem FilestoreFilesystem,
	resource SessionResource,
	mount *SessionFileMount,
) error {
	if resource.ResourceType != SessionResourceTypeFile {
		if mount != nil {
			return ErrPreconditionFailed
		}
		return nil
	}
	if mount == nil ||
		mount.ResourceExternalID != resource.ExternalID ||
		mount.Path == "/uploads" ||
		!filestorePathIsDescendant("/uploads", mount.Path) {
		return ErrPreconditionFailed
	}
	if err := validateFilestorePath(mount.Path); err != nil {
		return err
	}
	if err := rejectSessionFileMountConflictTx(ctx, tx, filesystem, mount.Path); err != nil {
		return err
	}

	file, err := getFileRecordSQLX(ctx, tx, `
		select `+fileSQLXColumns+`
		from files
		where workspace_uuid = :workspace_uuid
			and external_id = :file_external_id
			and deleted_at is null
			for share
	`, getFileArguments(filesystem.WorkspaceUUID, mount.FileExternalID))
	if errors.Is(err, ErrNotFound) {
		return ErrFileReferenceNotFound
	}
	if err != nil {
		return err
	}
	for _, directoryPath := range filestoreDirectoryChain(filestoreParentPath(mount.Path)) {
		if _, err := ensureFilestoreDirectoryTx(
			ctx,
			tx,
			filesystem,
			directoryPath,
			filestoreNow(resource.CreatedAt),
		); err != nil {
			return err
		}
	}
	entry, err := getFilestoreEntrySQLX(ctx, tx, `
		insert into filestore_entries (
			uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			kind, path, parent_path, size_bytes, media_type, metadata,
			authorization_metadata, tags, downloadable, md5, sha256,
			s3_bucket, s3_key, expires_at, managed_by,
			managed_resource_uuid, source_file_uuid,
			created_by_api_key_uuid, created_by_session_uuid,
			created_by_code_session_uuid, created_at, updated_at
		)
		values (
			gen_random_uuid(),
			concat('fse_', replace(CAST(gen_random_uuid() AS text), '-', '')),
			:organization_uuid, :workspace_uuid, :filesystem_uuid,
			'file', :entry_path, :parent_path, :size_bytes, :media_type,
			CAST('{}' AS jsonb), CAST('{}' AS jsonb), CAST(array[] AS text[]),
			:downloadable, null, :sha256, :s3_bucket, :s3_key, null,
			:managed_by, :managed_resource_uuid, :source_file_uuid,
			:created_by_api_key_uuid, :created_by_session_uuid,
			:created_by_code_session_uuid, :created_at, :created_at
		)
		returning `+filestoreEntryColumns()+`
	`, map[string]any{
		"organization_uuid":            dbUUID(filesystem.OrganizationUUID),
		"workspace_uuid":               dbUUID(filesystem.WorkspaceUUID),
		"filesystem_uuid":              dbUUID(filesystem.UUID),
		"entry_path":                   mount.Path,
		"parent_path":                  filestoreParentPath(mount.Path),
		"size_bytes":                   file.SizeBytes,
		"media_type":                   file.MimeType,
		"downloadable":                 file.Downloadable,
		"sha256":                       file.SHA256,
		"s3_bucket":                    file.S3Bucket,
		"s3_key":                       file.S3Key,
		"managed_by":                   sessionFileResourceManagedBy,
		"managed_resource_uuid":        dbUUID(resource.UUID),
		"source_file_uuid":             dbUUID(file.UUID),
		"created_by_api_key_uuid":      dbNullableUUID(filesystem.CreatedByAPIKeyUUID),
		"created_by_session_uuid":      dbUUID(filesystem.SessionUUID),
		"created_by_code_session_uuid": dbNullableUUID(filesystem.CodeSessionUUID),
		"created_at":                   filestoreNow(resource.CreatedAt),
	})
	if isUniqueViolation(err) {
		return ErrFilestorePathExists
	}
	if err != nil {
		return err
	}
	return upsertSessionFileProjectionTx(ctx, tx, session, entry.UUID, file, resource.CreatedAt)
}

func upsertSessionFileProjectionTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	entryUUID string,
	file FileRecord,
	createdAt time.Time,
) error {
	_, err := namedExecContext(ctx, tx, upsertSessionFileProjectionSQL, map[string]any{
		"file_uuid":               dbUUID(entryUUID),
		"workspace_uuid":          dbUUID(session.WorkspaceUUID),
		"filename":                file.Filename,
		"mime_type":               file.MimeType,
		"size_bytes":              file.SizeBytes,
		"sha256":                  file.SHA256,
		"s3_bucket":               file.S3Bucket,
		"s3_key":                  file.S3Key,
		"downloadable":            file.Downloadable,
		"scope_type":              sessionFileProjectionScope,
		"scope_id":                session.ExternalID,
		"created_by_api_key_uuid": dbUUID(session.CreatedByAPIKeyUUID),
		"created_at":              filestoreNow(createdAt),
	})
	return err
}

func refreshSessionFileProjectionForEntryTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	filesystem FilestoreFilesystem,
	entry FilestoreEntry,
) error {
	if filestoreEntryExpired(entry, time.Now().UTC()) {
		return softDeleteSessionFileProjectionByEntryTx(ctx, tx, workspaceUUID, entry.UUID)
	}
	if entry.Kind == FilestoreEntryKindFile &&
		filestorePathIsDescendant(sessionOutputsRootPath, entry.Path) {
		if entry.SizeBytes == nil || entry.SHA256 == nil ||
			entry.S3Bucket == nil || entry.S3Key == nil {
			return ErrPreconditionFailed
		}
		session, err := getSessionForFileProjectionTx(ctx, tx, workspaceUUID, filesystem.SessionUUID)
		if err != nil {
			return err
		}
		mimeType := filestoreString(entry.DetectedMimeType)
		if mimeType == "" {
			mimeType = filestoreString(entry.MediaType)
		}
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		return upsertSessionFileProjectionTx(ctx, tx, session, entry.UUID, FileRecord{
			Filename:     path.Base(entry.Path),
			MimeType:     mimeType,
			SizeBytes:    filestoreInt64(entry.SizeBytes),
			SHA256:       filestoreString(entry.SHA256),
			S3Bucket:     filestoreString(entry.S3Bucket),
			S3Key:        filestoreString(entry.S3Key),
			Downloadable: entry.Downloadable,
		}, entry.CreatedAt)
	}
	if entry.Kind == FilestoreEntryKindFile &&
		filestorePathIsDescendant(sessionUploadsRootPath, entry.Path) &&
		filestoreString(entry.ManagedBy) == sessionFileResourceManagedBy &&
		entry.ManagedResourceUUID != nil &&
		entry.SourceFileUUID != nil {
		session, err := getSessionForFileProjectionTx(ctx, tx, workspaceUUID, filesystem.SessionUUID)
		if err != nil {
			return err
		}
		source, err := getFileRecordSQLX(
			ctx,
			tx,
			getFileByUUIDQuery,
			fileUUIDArguments(filesystem.WorkspaceUUID, filestoreString(entry.SourceFileUUID)),
		)
		if err != nil {
			return err
		}
		return upsertSessionFileProjectionTx(
			ctx, tx, session, entry.UUID, source, entry.CreatedAt,
		)
	}
	return softDeleteSessionFileProjectionByEntryTx(ctx, tx, workspaceUUID, entry.UUID)
}

func getSessionForFileProjectionTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	sessionUUID string,
) (Session, error) {
	return getSessionSQLX(ctx, tx, `
		select `+sessionSQLXColumns+`
		from sessions
		where workspace_uuid = :workspace_uuid
			and uuid = :session_uuid
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"session_uuid":   dbUUID(sessionUUID),
	})
}

func softDeleteSessionFileProjectionByEntryTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	entryUUID string,
) error {
	_, err := namedExecContext(ctx, tx, softDeleteSessionFileProjectionByEntrySQL, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"scope_type":     sessionFileProjectionScope,
		"file_uuid":      dbUUID(entryUUID),
	})
	return err
}

func softDeleteSessionFileProjectionSubtreeTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	filesystem FilestoreFilesystem,
	rootPath string,
) error {
	_, err := namedExecContext(ctx, tx, softDeleteSessionFileProjectionSubtreeSQL, map[string]any{
		"workspace_uuid":  dbUUID(workspaceUUID),
		"scope_type":      sessionFileProjectionScope,
		"filesystem_uuid": dbUUID(filesystem.UUID),
		"root_path":       rootPath,
	})
	return err
}

func rejectSessionFileMountConflictTx(
	ctx context.Context,
	tx *sqlx.Tx,
	filesystem FilestoreFilesystem,
	path string,
) error {
	// The filesystem mutation lock is already held. This lookup only classifies
	// namespace conflicts owned by another Session File resource so the API can
	// return 400; ordinary occupied entries remain ErrFilestorePathExists/409.
	var conflictingPath string
	err := namedGetContext(ctx, tx, &conflictingPath, findSessionFileMountConflictSQL, map[string]any{
		"workspace_uuid":  dbUUID(filesystem.WorkspaceUUID),
		"filesystem_uuid": dbUUID(filesystem.UUID),
		"managed_by":      sessionFileResourceManagedBy,
		"entry_path":      path,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &SessionFileMountConflictError{
		Path:            path,
		ConflictingPath: conflictingPath,
	}
}

func getSessionResourceForMutationSQLX(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	sessionExternalID string,
	resourceExternalID string,
) (SessionResource, error) {
	var row sessionResourceRow
	err := namedGetContext(ctx, tx, &row, `
		select `+sessionResourceSQLXColumns+`
		from session_resources
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :resource_external_id
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid":       dbUUID(workspaceUUID),
		"session_external_id":  sessionExternalID,
		"resource_external_id": resourceExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SessionResource{}, ErrNotFound
	}
	if err != nil {
		return SessionResource{}, err
	}
	return row.resource(), nil
}

func unbindSessionFileResourceTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	resource SessionResource,
) error {
	if resource.ResourceType != SessionResourceTypeFile {
		return nil
	}
	filesystem, err := lockSessionFilestoreMutationTx(ctx, tx, session)
	if err != nil {
		return err
	}

	var entry filestoreEntryRow
	err = namedGetContext(ctx, tx, &entry, filestoreEntrySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and filesystem_uuid = :filesystem_uuid
			and managed_by = :managed_by
			and managed_resource_uuid = :resource_uuid
			and source_file_uuid is not null
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid":  dbUUID(filesystem.WorkspaceUUID),
		"filesystem_uuid": dbUUID(filesystem.UUID),
		"managed_by":      sessionFileResourceManagedBy,
		"resource_uuid":   dbUUID(resource.UUID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, `
		update filestore_entries
		set deleted_at = now(), updated_at = now()
		where uuid = :entry_uuid and deleted_at is null
	`, map[string]any{"entry_uuid": entry.UUID}); err != nil {
		return err
	}
	if _, err := namedExecContext(ctx, tx, softDeleteSessionFileProjectionSQL, map[string]any{
		"workspace_uuid": dbUUID(session.WorkspaceUUID),
		"file_uuid":      entry.UUID,
		"scope_type":     sessionFileProjectionScope,
		"scope_id":       session.ExternalID,
	}); err != nil {
		return err
	}
	return nil
}

func softDeleteSessionResourceSQLX(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceUUID string,
	sessionExternalID string,
	resourceExternalID string,
) error {
	result, err := namedExecContext(ctx, tx, `
		update session_resources
		set deleted_at = now(), updated_at = now()
		where workspace_uuid = :workspace_uuid
			and session_external_id = :session_external_id
			and external_id = :resource_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":       dbUUID(workspaceUUID),
		"session_external_id":  sessionExternalID,
		"resource_external_id": resourceExternalID,
	})
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
