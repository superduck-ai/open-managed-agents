package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

const (
	sessionOutputsRootPath       = "/outputs"
	sessionUploadsRootPath       = "/uploads"
	countSessionFileResourcesSQL = `
		select count(*)
		from session_resources
		where workspace_id = :workspace_id
			and session_external_id = :session_external_id
			and resource_type = :resource_type
			and deleted_at is null
	`
	findSessionFileMountConflictSQL = `
		select resource.path
		from session_resources resource
		cross join (values (CAST(:entry_path AS text))) as candidate(path)
		where resource.workspace_id = (
			select id from workspaces where uuid = CAST(:workspace_uuid AS uuid)
		)
			and resource.session_id = :session_id
			and resource.payload is not null
			and resource.deleted_at is null
			and (
				resource.path = candidate.path
				or left(resource.path, length(candidate.path) + 1) = concat(candidate.path, '/')
				or left(candidate.path, length(resource.path) + 1) = concat(resource.path, '/')
			)
		order by resource.path
		limit 1
	`
)

func enforceSessionFileResourceCapacityTx(
	ctx context.Context,
	tx *sqlx.Tx,
	workspaceID int64,
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
		"workspace_id":        workspaceID,
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

func bindSessionFileResourceWithLockedFilesystemTx(
	ctx context.Context,
	tx *sqlx.Tx,
	session Session,
	filesystem FilestoreFilesystem,
	resource SessionResource,
	mount *SessionFileMount,
) (SessionResource, error) {
	if resource.ResourceType != SessionResourceTypeFile {
		if mount != nil {
			return SessionResource{}, ErrPreconditionFailed
		}
		return resource, nil
	}
	if mount == nil ||
		mount.ResourceExternalID != resource.ExternalID ||
		mount.Path == "/uploads" ||
		!filestorePathIsDescendant("/uploads", mount.Path) {
		return SessionResource{}, ErrPreconditionFailed
	}
	if err := validateFilestorePath(mount.Path); err != nil {
		return SessionResource{}, err
	}
	if err := rejectSessionFileMountConflictTx(ctx, tx, filesystem, mount.Path); err != nil {
		return SessionResource{}, err
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
		return SessionResource{}, ErrFileReferenceNotFound
	}
	if err != nil {
		return SessionResource{}, err
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
			return SessionResource{}, err
		}
	}
	var row sessionResourceRow
	err = namedGetContext(ctx, tx, &row, `
		update session_resources
		set path = :entry_path, parent_path = :parent_path,
			file_uuid = CAST(:file_uuid AS uuid),
			updated_at = :updated_at
		where id = :resource_id and workspace_id = :workspace_id
			and session_id = :session_id and deleted_at is null
		returning `+sessionResourceSQLXColumns+`
	`, map[string]any{
		"entry_path":   mount.Path,
		"parent_path":  filestoreParentPath(mount.Path),
		"file_uuid":    file.UUID,
		"updated_at":   filestoreNow(resource.CreatedAt),
		"resource_id":  resource.ID,
		"workspace_id": session.WorkspaceID,
		"session_id":   session.ID,
	})
	if isUniqueViolation(err) {
		return SessionResource{}, ErrFilestorePathExists
	}
	if err != nil {
		return SessionResource{}, err
	}
	return row.resource(), nil
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
		"workspace_uuid": filesystem.WorkspaceUUID,
		"session_id":     filesystem.SessionID,
		"entry_path":     path,
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
			and payload is not null
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
	if resource.ResourceType != SessionResourceTypeFile {
		return nil
	}
	_, err := lockSessionFilestoreMutationTx(ctx, tx, session)
	return err
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
