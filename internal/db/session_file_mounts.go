package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/superduck-ai/yourbatis"
)

func enforceSessionFileResourceCapacityTx(
	ctx context.Context,
	executor yourbatis.Executor,
	workspaceUUID string,
	sessionExternalID string,
	additionalFiles int,
) error {
	// Resource mutations hold the owning Session row lock. Session creation
	// creates that row in the same transaction before checking capacity.
	if additionalFiles == 0 {
		return nil
	}
	mapper := NewSessionResourceMapper(executor)
	activeFiles, err := mapper.CountSessionFileResources(
		ctx,
		workspaceUUID,
		sessionExternalID,
		SessionResourceTypeFile,
	)
	if err != nil {
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
	executor yourbatis.Executor,
	session Session,
) (FilestoreFilesystem, error) {
	storageMapper := NewWorkspaceStorageUsageMapper(executor)
	if err := storageMapper.LockWorkspace(ctx, session.WorkspaceUUID); err != nil {
		return FilestoreFilesystem{}, err
	}
	filesystemMapper := NewFilestoreFilesystemMapper(executor)
	row, found, err := filesystemMapper.FindSessionFilesystemForMutation(ctx, session.WorkspaceUUID, session.UUID)
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if !found {
		return FilestoreFilesystem{}, ErrNotFound
	}
	filesystem, err := row.filesystem()
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	if err := filesystemMapper.LockFilesystem(ctx, filesystem.UUID); err != nil {
		return FilestoreFilesystem{}, err
	}
	return filesystem, nil
}

func bindSessionFileResourceWithLockedFilesystemTx(
	ctx context.Context,
	executor yourbatis.Executor,
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
		mount.MountPath == "" ||
		mount.Path == "/uploads" ||
		!filestorePathIsDescendant("/uploads", mount.Path) {
		return SessionResource{}, ErrPreconditionFailed
	}
	if err := validateFilestorePath(mount.Path); err != nil {
		return SessionResource{}, err
	}
	if err := rejectSessionFileMountConflictTx(ctx, executor, filesystem, mount.Path); err != nil {
		return SessionResource{}, err
	}

	fileMapper := NewFileMapper(executor)
	fileRow, err := fileMapper.GetFileForShare(ctx, session.WorkspaceUUID, mount.FileExternalID)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionResource{}, ErrFileReferenceNotFound
	}
	if err != nil {
		return SessionResource{}, err
	}
	file := fileRow.record()
	for _, directoryPath := range filestoreDirectoryChain(filestoreParentPath(mount.Path)) {
		if _, err := ensureFilestoreDirectoryTx(
			ctx,
			executor,
			filesystem,
			directoryPath,
			filestoreNow(resource.CreatedAt),
		); err != nil {
			return SessionResource{}, err
		}
	}
	resourceMapper := NewSessionResourceMapper(executor)
	row, err := resourceMapper.BindSessionFileResource(ctx, sessionFileResourceBindingParams{
		EntryPath:     mount.Path,
		ParentPath:    filestoreParentPath(mount.Path),
		MountPath:     mount.MountPath,
		FileUUID:      file.UUID,
		UpdatedAt:     filestoreNow(resource.CreatedAt),
		ResourceUUID:  resource.UUID,
		WorkspaceUUID: session.WorkspaceUUID,
		SessionUUID:   session.UUID,
	})
	if isUniqueViolation(err) {
		return SessionResource{}, ErrFilestorePathExists
	}
	if err != nil {
		return SessionResource{}, err
	}
	bound := row.resource()
	bound.File = &SessionResourceFileReference{
		FileID:        file.ExternalID,
		NamespacePath: mount.Path,
		MountPath:     mount.MountPath,
		Ownership:     SessionResourceFileOwnershipReferenced,
	}
	return bound, nil
}

func rejectSessionFileMountConflictTx(
	ctx context.Context,
	executor yourbatis.Executor,
	filesystem FilestoreFilesystem,
	path string,
) error {
	// The filesystem mutation lock is already held. This lookup only classifies
	// namespace conflicts owned by another Session File resource so the API can
	// return 400; ordinary occupied entries remain ErrFilestorePathExists/409.
	mapper := NewSessionResourceMapper(executor)
	conflictingPath, found, err := mapper.FindMountConflict(ctx, sessionResourcePathParams{
		WorkspaceUUID: filesystem.WorkspaceUUID,
		SessionUUID:   filesystem.SessionUUID,
		EntryPath:     path,
	})
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return &SessionFileMountConflictError{
		Path:            path,
		ConflictingPath: conflictingPath,
	}
}

func getSessionResourceForMutation(
	ctx context.Context,
	executor yourbatis.Executor,
	workspaceUUID string,
	sessionExternalID string,
	resourceExternalID string,
) (SessionResource, error) {
	mapper := NewSessionResourceMapper(executor)
	row, err := mapper.GetSessionResourceForMutation(
		ctx,
		workspaceUUID,
		sessionExternalID,
		resourceExternalID,
	)
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
	executor yourbatis.Executor,
	session Session,
	resource SessionResource,
) error {
	if resource.ResourceType != SessionResourceTypeFile {
		return nil
	}
	_, err := lockSessionFilestoreMutationTx(ctx, executor, session)
	return err
}

func softDeleteSessionResource(
	ctx context.Context,
	executor yourbatis.Executor,
	workspaceUUID string,
	sessionExternalID string,
	resourceExternalID string,
) error {
	mapper := NewSessionResourceMapper(executor)
	affected, err := mapper.SoftDeleteSessionResource(
		ctx,
		workspaceUUID,
		sessionExternalID,
		resourceExternalID,
	)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
