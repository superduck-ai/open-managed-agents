package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/jmoiron/sqlx"
)

const sessionFileResourceManagedBy = "session_file_resource"

func sessionFileMountsByResource(mounts []SessionFileMount) (map[string]SessionFileMount, error) {
	byResource := make(map[string]SessionFileMount, len(mounts))
	for _, mount := range mounts {
		if strings.TrimSpace(mount.ResourceExternalID) == "" ||
			strings.TrimSpace(mount.FileExternalID) == "" ||
			strings.TrimSpace(mount.Path) == "" {
			return nil, ErrPreconditionFailed
		}
		if _, exists := byResource[mount.ResourceExternalID]; exists {
			return nil, ErrPreconditionFailed
		}
		byResource[mount.ResourceExternalID] = mount
	}
	return byResource, nil
}

func bindSessionResourceFileTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	resource SessionResource,
	mount *SessionFileMount,
) error {
	if resource.ResourceType != FilestoreEntryKindFile {
		if mount != nil {
			return ErrPreconditionFailed
		}
		return nil
	}
	filesystem, err := lockSessionFilestoreMutationTx(ctx, tx, session)
	if err != nil {
		return err
	}
	return bindSessionFileResourceTx(ctx, tx, session, filesystem, resource, mount)
}

func lockSessionFilestoreMutationTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
) (FilestoreFilesystem, error) {
	if _, err := namedExecContext(ctx, tx, fileWorkspaceLockQuery, map[string]any{
		"workspace_id": session.WorkspaceID,
	}); err != nil {
		return FilestoreFilesystem{}, err
	}
	filesystem, err := getFilestoreFilesystemSQLX(ctx, tx, filestoreFilesystemSelectSQL()+`
		where workspace_uuid = (
			select uuid from workspaces where id = :workspace_id
		)
			and session_uuid = :session_uuid
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_id": session.WorkspaceID,
		"session_uuid": session.UUID,
	})
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if _, err := namedExecContext(ctx, tx, `
		select pg_advisory_xact_lock(-CAST(:filesystem_id AS bigint))
	`, map[string]any{"filesystem_id": filesystem.ID}); err != nil {
		return FilestoreFilesystem{}, err
	}
	return filesystem, nil
}

func bindSessionFileResourceTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	filesystem FilestoreFilesystem,
	resource SessionResource,
	mount *SessionFileMount,
) error {
	if resource.ResourceType != FilestoreEntryKindFile {
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

	file, err := getFileRecordSQLX(ctx, tx, `
		select `+fileSQLXColumns+`
		from files
		where workspace_id = :workspace_id
			and external_id = :file_external_id
			and deleted_at is null
		for share
	`, getFileArguments(session.WorkspaceID, mount.FileExternalID))
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
			session.WorkspaceID,
			filesystem,
			directoryPath,
			filestoreNow(resource.CreatedAt),
		); err != nil {
			return err
		}
	}
	_, err = getFilestoreEntrySQLX(ctx, tx, `
		insert into filestore_entries (
			uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
			kind, path, parent_path, size_bytes, media_type, metadata,
			authorization_metadata, tags, downloadable, md5, sha256,
			s3_bucket, s3_key, expires_at, managed_by,
			managed_resource_external_id, source_file_uuid,
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
			:managed_by, :managed_resource_external_id, :source_file_uuid,
			:created_by_api_key_uuid, :created_by_session_uuid,
			:created_by_code_session_uuid, :created_at, :created_at
		)
		returning `+filestoreEntryColumns()+`
	`, map[string]any{
		"organization_uuid":            filesystem.OrganizationUUID,
		"workspace_uuid":               filesystem.WorkspaceUUID,
		"filesystem_uuid":              filesystem.UUID,
		"entry_path":                   mount.Path,
		"parent_path":                  filestoreParentPath(mount.Path),
		"size_bytes":                   file.SizeBytes,
		"media_type":                   file.MimeType,
		"downloadable":                 file.Downloadable,
		"sha256":                       file.SHA256,
		"s3_bucket":                    file.S3Bucket,
		"s3_key":                       file.S3Key,
		"managed_by":                   sessionFileResourceManagedBy,
		"managed_resource_external_id": resource.ExternalID,
		"source_file_uuid":             file.UUID,
		"created_by_api_key_uuid":      filesystem.CreatedByAPIKeyUUID,
		"created_by_session_uuid":      filesystem.SessionUUID,
		"created_by_code_session_uuid": filesystem.CodeSessionUUID,
		"created_at":                   filestoreNow(resource.CreatedAt),
	})
	if isUniqueViolation(err) {
		return ErrFilestorePathExists
	}
	return err
}

func getSessionResourceForMutationSQLX(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
	sessionExternalID string,
	resourceExternalID string,
) (SessionResource, error) {
	var row sessionResourceRow
	err := namedGetContext(ctx, tx, &row, `
		select `+sessionResourceSQLXColumns+`
		from session_resources
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and external_id = :resource_external_id
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_id":         workspaceID,
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
	if resource.ResourceType != FilestoreEntryKindFile {
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
			and managed_resource_external_id = :resource_external_id
			and source_file_uuid is not null
			and deleted_at is null
		for update
	`, map[string]any{
		"workspace_uuid":       filesystem.WorkspaceUUID,
		"filesystem_uuid":      filesystem.UUID,
		"managed_by":           sessionFileResourceManagedBy,
		"resource_external_id": resource.ExternalID,
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
		where id = :entry_id and deleted_at is null
	`, map[string]any{"entry_id": entry.ID}); err != nil {
		return err
	}
	return nil
}

func softDeleteSessionResourceSQLX(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
	sessionExternalID string,
	resourceExternalID string,
) error {
	result, err := namedExecContext(ctx, tx, `
		update session_resources
		set deleted_at = now(), updated_at = now()
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and external_id = :resource_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_id":         workspaceID,
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
