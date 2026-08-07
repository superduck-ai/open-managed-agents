package db

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/superduck-ai/yourbatis"
)

type MemoryStore struct {
	UUID                string
	ExternalID          string
	OrganizationUUID    string
	WorkspaceUUID       string
	CreatedByAPIKeyUUID string
	Name                string
	Description         string
	Metadata            json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ArchivedAt          *time.Time
	DeletedAt           *time.Time
}

type Memory struct {
	UUID                     string
	ExternalID               string
	OrganizationUUID         string
	WorkspaceUUID            string
	MemoryStoreUUID          string
	MemoryStoreExternalID    string
	CurrentVersionUUID       string
	CurrentVersionExternalID string
	Path                     string
	ContentSizeBytes         int64
	ContentSHA256            string
	S3Bucket                 string
	S3Key                    string
	CreatedAt                time.Time
	UpdatedAt                time.Time
	DeletedAt                *time.Time
}

type MemoryActor struct {
	Type             string
	APIKeyUUID       string
	APIKeyExternalID string
	SessionID        string
	UserID           string
}

type MemoryVersion struct {
	UUID                  string
	ExternalID            string
	OrganizationUUID      string
	WorkspaceUUID         string
	MemoryStoreUUID       string
	MemoryStoreExternalID string
	MemoryUUID            string
	MemoryExternalID      string
	Operation             string
	Path                  *string
	ContentSizeBytes      *int64
	ContentSHA256         *string
	S3Bucket              *string
	S3Key                 *string
	CreatedBy             MemoryActor
	RedactedAt            *time.Time
	RedactedBy            *MemoryActor
	CreatedAt             time.Time
}

type ObjectRef struct {
	WorkspaceUUID string
	Bucket        string
	Key           string
	ResourceType  string
	ResourceID    string
}

type MemoryStorePageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type MemoryPageCursor struct {
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
	UUID      string
}

type MemoryVersionPageCursor struct {
	CreatedAt time.Time
	UUID      string
}

type ListMemoryStoresPageParams struct {
	WorkspaceUUID   string
	Limit           int
	Cursor          *MemoryStorePageCursor
	IncludeArchived bool
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type ListMemoriesPageParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	Limit                 int
	Cursor                *MemoryPageCursor
	PathPrefix            string
	Order                 string
	OrderBy               string
}

type ListMemoryVersionsPageParams struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	Limit                 int
	Cursor                *MemoryVersionPageCursor
	MemoryExternalID      string
	Operation             string
	APIKeyExternalID      string
	SessionID             string
	CreatedAtGTE          *time.Time
	CreatedAtLTE          *time.Time
}

type UpdateMemoryInput struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	MemoryExternalID      string
	VersionUUID           string
	VersionExternalID     string
	Path                  *string
	ContentProvided       bool
	ContentSizeBytes      int64
	ContentSHA256         string
	S3Bucket              string
	S3Key                 string
	ExpectedContentSHA256 *string
	BaseVersionExternalID string
	Actor                 MemoryActor
	Now                   time.Time
}

type DeleteMemoryInput struct {
	WorkspaceUUID         string
	MemoryStoreExternalID string
	MemoryExternalID      string
	VersionUUID           string
	VersionExternalID     string
	ExpectedContentSHA256 *string
	Actor                 MemoryActor
	Now                   time.Time
}

type MemoryMutationResult struct {
	Memory         Memory
	VersionCreated bool
}

type MemoryPathConflictError struct {
	ConflictingMemoryID string
	ConflictingPath     string
}

func (e *MemoryPathConflictError) Error() string {
	return "memory path conflicts with existing memory"
}

func (d *DB) CreateMemoryStore(ctx context.Context, store MemoryStore) (MemoryStore, error) {
	mapper := NewMemoryStoreMapper(d.mapperDB)
	row, err := mapper.Insert(ctx, insertMemoryStoreParams{
		UUID:                store.UUID,
		ExternalID:          store.ExternalID,
		OrganizationUUID:    store.OrganizationUUID,
		WorkspaceUUID:       store.WorkspaceUUID,
		CreatedByAPIKeyUUID: store.CreatedByAPIKeyUUID,
		Name:                store.Name,
		Description:         store.Description,
		Metadata:            memoryJSONArg(store.Metadata),
		CreatedAt:           store.CreatedAt,
	})
	return memoryStoreFromMapperRow(row, err)
}

func (d *DB) GetMemoryStore(ctx context.Context, workspaceUUID, externalID string) (MemoryStore, error) {
	mapper := NewMemoryStoreMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, externalID)
	return memoryStoreFromMapperRow(row, err)
}

func (d *DB) GetMemoryStoreByExternalID(ctx context.Context, organizationUUID, externalID string) (MemoryStore, error) {
	mapper := NewMemoryStoreMapper(d.mapperDB)
	row, err := mapper.FindByOrganizationAndExternalID(ctx, organizationUUID, externalID)
	return memoryStoreFromMapperRow(row, err)
}

func (d *DB) UpdateMemoryStore(ctx context.Context, workspaceUUID, externalID string, next MemoryStore) (MemoryStore, error) {
	var updated MemoryStore
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		mapper := NewMemoryStoreMapper(executor)
		row, txErr := mapper.FindForUpdate(ctx, workspaceUUID, externalID)
		current, txErr := memoryStoreFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}
		if current.ArchivedAt != nil {
			return ErrInvalidState
		}

		row, txErr = mapper.UpdateByExternalID(ctx, updateMemoryStoreParams{
			WorkspaceUUID: workspaceUUID,
			ExternalID:    externalID,
			Name:          next.Name,
			Description:   next.Description,
			Metadata:      memoryJSONArg(next.Metadata),
			UpdatedAt:     next.UpdatedAt,
		})
		updated, txErr = memoryStoreFromMapperRow(row, txErr)
		return txErr
	})
	return updated, err
}

func (d *DB) ArchiveMemoryStore(ctx context.Context, workspaceUUID, externalID string) (MemoryStore, error) {
	mapper := NewMemoryStoreMapper(d.mapperDB)
	row, err := mapper.ArchiveByExternalID(ctx, workspaceUUID, externalID)
	return memoryStoreFromMapperRow(row, err)
}

func (d *DB) DeleteMemoryStore(ctx context.Context, workspaceUUID, externalID string) ([]ObjectRef, error) {
	var refs []ObjectRef
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		storeMapper := NewMemoryStoreMapper(executor)
		memoryMapper := NewMemoryMapper(executor)
		versionMapper := NewMemoryVersionMapper(executor)

		storeUUID, txErr := storeMapper.FindUUIDForUpdate(ctx, workspaceUUID, externalID)
		if txErr != nil {
			return mapNoRows(txErr)
		}
		rows, txErr := versionMapper.ListObjectRefsByStoreUUID(ctx, workspaceUUID, storeUUID)
		if txErr != nil {
			return txErr
		}
		refs = memoryObjectRefsFromMapperRows(rows)
		if txErr = versionMapper.DeleteByStoreUUID(ctx, workspaceUUID, storeUUID); txErr != nil {
			return txErr
		}
		if txErr = memoryMapper.DeleteByStoreUUID(ctx, workspaceUUID, storeUUID); txErr != nil {
			return txErr
		}
		return storeMapper.DeleteByUUID(ctx, workspaceUUID, storeUUID)
	})
	return refs, err
}

func (d *DB) ListMemoryStoresPage(ctx context.Context, params ListMemoryStoresPageParams) ([]MemoryStore, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	filter := listMemoryStoresParams{
		WorkspaceUUID:   params.WorkspaceUUID,
		Limit:           params.Limit + 1,
		IncludeArchived: params.IncludeArchived,
	}
	if params.CreatedAtGTE != nil {
		filter.HasCreatedAtGTE = true
		filter.CreatedAtGTE = *params.CreatedAtGTE
	}
	if params.CreatedAtLTE != nil {
		filter.HasCreatedAtLTE = true
		filter.CreatedAtLTE = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		filter.HasCursor = true
		filter.CursorCreatedAt = params.Cursor.CreatedAt
		filter.CursorUUID = params.Cursor.UUID
	}

	mapper := NewMemoryStoreMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	stores := memoryStoresFromMapperRows(rows)
	hasMore := len(stores) > params.Limit
	if hasMore {
		stores = stores[:params.Limit]
	}
	return stores, hasMore, nil
}

func (d *DB) CreateMemory(ctx context.Context, memory Memory, version MemoryVersion) (Memory, error) {
	var created Memory
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		storeMapper := NewMemoryStoreMapper(executor)
		memoryMapper := NewMemoryMapper(executor)
		versionMapper := NewMemoryVersionMapper(executor)

		storeRow, txErr := storeMapper.FindForUpdate(ctx, memory.WorkspaceUUID, memory.MemoryStoreExternalID)
		store, txErr := memoryStoreFromMapperRow(storeRow, txErr)
		if txErr != nil {
			return txErr
		}
		if store.ArchivedAt != nil {
			return ErrInvalidState
		}
		if txErr = ensureMemoryPathAvailable(ctx, memoryMapper, store.WorkspaceUUID, store.UUID, memory.Path, ""); txErr != nil {
			return txErr
		}

		row, txErr := memoryMapper.Insert(ctx, insertMemoryParams{
			UUID:                     memory.UUID,
			ExternalID:               memory.ExternalID,
			OrganizationUUID:         store.OrganizationUUID,
			WorkspaceUUID:            store.WorkspaceUUID,
			MemoryStoreUUID:          store.UUID,
			MemoryStoreExternalID:    store.ExternalID,
			CurrentVersionExternalID: version.ExternalID,
			Path:                     memory.Path,
			ContentSizeBytes:         memory.ContentSizeBytes,
			ContentSHA256:            memory.ContentSHA256,
			S3Bucket:                 memory.S3Bucket,
			S3Key:                    memory.S3Key,
			CreatedAt:                memory.CreatedAt,
		})
		if isUniqueViolation(txErr) {
			return memoryPathConflict(ctx, memoryMapper, store.WorkspaceUUID, store.UUID, memory.Path)
		}
		insertedMemory, txErr := memoryFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}

		version.OrganizationUUID = store.OrganizationUUID
		version.WorkspaceUUID = store.WorkspaceUUID
		version.MemoryStoreUUID = store.UUID
		version.MemoryStoreExternalID = store.ExternalID
		version.MemoryUUID = insertedMemory.UUID
		version.MemoryExternalID = insertedMemory.ExternalID
		insertedVersion, txErr := insertMemoryVersion(ctx, versionMapper, version)
		if txErr != nil {
			return txErr
		}
		row, txErr = memoryMapper.UpdateCurrentVersion(ctx, updateMemoryCurrentVersionParams{
			WorkspaceUUID:     store.WorkspaceUUID,
			MemoryUUID:        insertedMemory.UUID,
			VersionUUID:       insertedVersion.UUID,
			VersionExternalID: insertedVersion.ExternalID,
		})
		created, txErr = memoryFromMapperRow(row, txErr)
		return txErr
	})
	return created, err
}

func (d *DB) GetMemory(ctx context.Context, workspaceUUID, memoryStoreExternalID, memoryExternalID string) (Memory, error) {
	mapper := NewMemoryMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, memoryStoreExternalID, memoryExternalID)
	return memoryFromMapperRow(row, err)
}

func (d *DB) UpdateMemory(ctx context.Context, input UpdateMemoryInput) (MemoryMutationResult, error) {
	var result MemoryMutationResult
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		storeMapper := NewMemoryStoreMapper(executor)
		memoryMapper := NewMemoryMapper(executor)
		versionMapper := NewMemoryVersionMapper(executor)

		current, storeArchived, txErr := getActiveMemoryForMutation(
			ctx,
			storeMapper,
			memoryMapper,
			input.WorkspaceUUID,
			input.MemoryStoreExternalID,
			input.MemoryExternalID,
		)
		if txErr != nil {
			return txErr
		}
		if storeArchived {
			return ErrInvalidState
		}

		targetPath := current.Path
		if input.Path != nil {
			targetPath = *input.Path
		}
		targetSize := current.ContentSizeBytes
		targetSHA := current.ContentSHA256
		targetBucket := current.S3Bucket
		targetKey := current.S3Key
		if input.ContentProvided {
			targetSize = input.ContentSizeBytes
			targetSHA = input.ContentSHA256
			targetBucket = input.S3Bucket
			targetKey = input.S3Key
		}
		if input.ExpectedContentSHA256 != nil && current.ContentSHA256 != *input.ExpectedContentSHA256 {
			if current.Path == targetPath && current.ContentSHA256 == targetSHA {
				result.Memory = current
				return nil
			}
			return ErrPreconditionFailed
		}
		if input.BaseVersionExternalID != "" && current.CurrentVersionExternalID != input.BaseVersionExternalID {
			return ErrVersionConflict
		}
		if current.Path == targetPath && current.ContentSHA256 == targetSHA {
			result.Memory = current
			return nil
		}
		if targetPath != current.Path {
			if txErr = ensureMemoryPathAvailable(
				ctx,
				memoryMapper,
				current.WorkspaceUUID,
				current.MemoryStoreUUID,
				targetPath,
				current.UUID,
			); txErr != nil {
				return txErr
			}
		}

		pathValue := targetPath
		contentSize := targetSize
		contentSHA := targetSHA
		bucket := targetBucket
		key := targetKey
		version, txErr := insertMemoryVersion(ctx, versionMapper, MemoryVersion{
			UUID:                  input.VersionUUID,
			ExternalID:            input.VersionExternalID,
			OrganizationUUID:      current.OrganizationUUID,
			WorkspaceUUID:         current.WorkspaceUUID,
			MemoryStoreUUID:       current.MemoryStoreUUID,
			MemoryStoreExternalID: current.MemoryStoreExternalID,
			MemoryUUID:            current.UUID,
			MemoryExternalID:      current.ExternalID,
			Operation:             "modified",
			Path:                  &pathValue,
			ContentSizeBytes:      &contentSize,
			ContentSHA256:         &contentSHA,
			S3Bucket:              &bucket,
			S3Key:                 &key,
			CreatedBy:             input.Actor,
			CreatedAt:             input.Now,
		})
		if txErr != nil {
			return txErr
		}
		row, txErr := memoryMapper.UpdateByExternalID(ctx, updateMemoryParams{
			WorkspaceUUID:         input.WorkspaceUUID,
			MemoryStoreExternalID: input.MemoryStoreExternalID,
			MemoryExternalID:      input.MemoryExternalID,
			VersionUUID:           version.UUID,
			VersionExternalID:     version.ExternalID,
			Path:                  targetPath,
			ContentSizeBytes:      targetSize,
			ContentSHA256:         targetSHA,
			S3Bucket:              targetBucket,
			S3Key:                 targetKey,
			UpdatedAt:             input.Now,
		})
		if isUniqueViolation(txErr) {
			return memoryPathConflict(ctx, memoryMapper, current.WorkspaceUUID, current.MemoryStoreUUID, targetPath)
		}
		updated, txErr := memoryFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}
		result = MemoryMutationResult{Memory: updated, VersionCreated: true}
		return nil
	})
	return result, err
}

func (d *DB) DeleteMemory(ctx context.Context, input DeleteMemoryInput) error {
	return d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		storeMapper := NewMemoryStoreMapper(executor)
		memoryMapper := NewMemoryMapper(executor)
		versionMapper := NewMemoryVersionMapper(executor)

		current, storeArchived, err := getActiveMemoryForMutation(
			ctx,
			storeMapper,
			memoryMapper,
			input.WorkspaceUUID,
			input.MemoryStoreExternalID,
			input.MemoryExternalID,
		)
		if err != nil {
			return err
		}
		if storeArchived {
			return ErrInvalidState
		}
		if input.ExpectedContentSHA256 != nil && current.ContentSHA256 != *input.ExpectedContentSHA256 {
			return ErrPreconditionFailed
		}

		pathValue := current.Path
		version, err := insertMemoryVersion(ctx, versionMapper, MemoryVersion{
			UUID:                  input.VersionUUID,
			ExternalID:            input.VersionExternalID,
			OrganizationUUID:      current.OrganizationUUID,
			WorkspaceUUID:         current.WorkspaceUUID,
			MemoryStoreUUID:       current.MemoryStoreUUID,
			MemoryStoreExternalID: current.MemoryStoreExternalID,
			MemoryUUID:            current.UUID,
			MemoryExternalID:      current.ExternalID,
			Operation:             "deleted",
			Path:                  &pathValue,
			CreatedBy:             input.Actor,
			CreatedAt:             input.Now,
		})
		if err != nil {
			return err
		}
		rowsAffected, err := memoryMapper.SoftDeleteByExternalID(ctx, deleteMemoryParams{
			WorkspaceUUID:         input.WorkspaceUUID,
			MemoryStoreExternalID: input.MemoryStoreExternalID,
			MemoryExternalID:      input.MemoryExternalID,
			VersionUUID:           version.UUID,
			VersionExternalID:     version.ExternalID,
			UpdatedAt:             input.Now,
		})
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (d *DB) ListMemoriesPage(ctx context.Context, params ListMemoriesPageParams) ([]Memory, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceUUID, params.MemoryStoreExternalID); err != nil {
		return nil, false, err
	}
	orderBy := params.OrderBy
	if orderBy == "" {
		orderBy = "path"
	}
	order := params.Order
	if order == "" {
		order = "asc"
	}
	filter := listMemoriesParams{
		WorkspaceUUID:         params.WorkspaceUUID,
		MemoryStoreExternalID: params.MemoryStoreExternalID,
		Limit:                 params.Limit + 1,
		PathPrefix:            params.PathPrefix,
		OrderBy:               orderBy,
		Descending:            order == "desc",
	}
	if params.Cursor != nil {
		filter.HasCursor = true
		filter.CursorPath = params.Cursor.Path
		filter.CursorCreatedAt = params.Cursor.CreatedAt
		filter.CursorUpdatedAt = params.Cursor.UpdatedAt
		filter.CursorUUID = params.Cursor.UUID
	}

	mapper := NewMemoryMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	memories := memoriesFromMapperRows(rows)
	hasMore := len(memories) > params.Limit
	if hasMore {
		memories = memories[:params.Limit]
	}
	return memories, hasMore, nil
}

func (d *DB) ListMemoriesForDepth(ctx context.Context, params ListMemoriesPageParams) ([]Memory, error) {
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceUUID, params.MemoryStoreExternalID); err != nil {
		return nil, err
	}
	mapper := NewMemoryMapper(d.mapperDB)
	rows, err := mapper.ListForDepth(ctx, listMemoriesForDepthParams{
		WorkspaceUUID:         params.WorkspaceUUID,
		MemoryStoreExternalID: params.MemoryStoreExternalID,
		PathPrefix:            params.PathPrefix,
	})
	if err != nil {
		return nil, err
	}
	return memoriesFromMapperRows(rows), nil
}

func (d *DB) GetMemoryVersion(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionExternalID string) (MemoryVersion, error) {
	mapper := NewMemoryVersionMapper(d.mapperDB)
	row, err := mapper.FindByExternalID(ctx, workspaceUUID, memoryStoreExternalID, versionExternalID)
	return memoryVersionFromMapperRow(row, err)
}

func (d *DB) ListMemoryVersionsPage(ctx context.Context, params ListMemoryVersionsPageParams) ([]MemoryVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceUUID, params.MemoryStoreExternalID); err != nil {
		return nil, false, err
	}
	filter := listMemoryVersionsParams{
		WorkspaceUUID:         params.WorkspaceUUID,
		MemoryStoreExternalID: params.MemoryStoreExternalID,
		Limit:                 params.Limit + 1,
		MemoryExternalID:      params.MemoryExternalID,
		Operation:             params.Operation,
		APIKeyExternalID:      params.APIKeyExternalID,
		SessionID:             params.SessionID,
	}
	if params.CreatedAtGTE != nil {
		filter.HasCreatedAtGTE = true
		filter.CreatedAtGTE = *params.CreatedAtGTE
	}
	if params.CreatedAtLTE != nil {
		filter.HasCreatedAtLTE = true
		filter.CreatedAtLTE = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		filter.HasCursor = true
		filter.CursorCreatedAt = params.Cursor.CreatedAt
		filter.CursorUUID = params.Cursor.UUID
	}

	mapper := NewMemoryVersionMapper(d.mapperDB)
	rows, err := mapper.ListPage(ctx, filter)
	if err != nil {
		return nil, false, err
	}
	versions := memoryVersionsFromMapperRows(rows)
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) RedactMemoryVersion(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionExternalID string, actor MemoryActor, now time.Time) (MemoryVersion, *ObjectRef, error) {
	var updated MemoryVersion
	var ref *ObjectRef
	err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		memoryMapper := NewMemoryMapper(executor)
		versionMapper := NewMemoryVersionMapper(executor)

		row, txErr := versionMapper.FindForUpdate(ctx, workspaceUUID, memoryStoreExternalID, versionExternalID)
		version, txErr := memoryVersionFromMapperRow(row, txErr)
		if txErr != nil {
			return txErr
		}
		activeHead, txErr := memoryMapper.CountActiveHead(ctx, workspaceUUID, memoryStoreExternalID, version.UUID)
		if txErr != nil {
			return txErr
		}
		if activeHead > 0 {
			return ErrInvalidState
		}
		if version.RedactedAt != nil {
			updated = version
			return nil
		}

		if version.S3Key != nil && *version.S3Key != "" {
			ref = &ObjectRef{
				WorkspaceUUID: version.WorkspaceUUID,
				ResourceType:  "memory_version",
				ResourceID:    version.ExternalID,
				Key:           *version.S3Key,
			}
			if version.S3Bucket != nil {
				ref.Bucket = *version.S3Bucket
			}
		}
		row, txErr = versionMapper.RedactByExternalID(ctx, redactMemoryVersionParams{
			WorkspaceUUID:              workspaceUUID,
			MemoryStoreExternalID:      memoryStoreExternalID,
			VersionExternalID:          versionExternalID,
			RedactedAt:                 now,
			RedactedByActorType:        actor.Type,
			RedactedByAPIKeyUUID:       optionalMemoryString(actor.APIKeyUUID),
			RedactedByAPIKeyExternalID: optionalMemoryString(actor.APIKeyExternalID),
			RedactedBySessionID:        optionalMemoryString(actor.SessionID),
			RedactedByUserID:           optionalMemoryString(actor.UserID),
		})
		updated, txErr = memoryVersionFromMapperRow(row, txErr)
		return txErr
	})
	return updated, ref, err
}

func getActiveMemoryForMutation(
	ctx context.Context,
	storeMapper MemoryStoreMapper,
	memoryMapper MemoryMapper,
	workspaceUUID string,
	memoryStoreExternalID string,
	memoryExternalID string,
) (Memory, bool, error) {
	storeRow, err := storeMapper.FindForUpdate(ctx, workspaceUUID, memoryStoreExternalID)
	store, err := memoryStoreFromMapperRow(storeRow, err)
	if err != nil {
		return Memory{}, false, err
	}
	memoryRow, err := memoryMapper.FindForUpdate(ctx, workspaceUUID, memoryStoreExternalID, memoryExternalID)
	memory, err := memoryFromMapperRow(memoryRow, err)
	if err != nil {
		return Memory{}, false, err
	}
	return memory, store.ArchivedAt != nil, nil
}

func (d *DB) ensureMemoryStoreExists(ctx context.Context, workspaceUUID, memoryStoreExternalID string) error {
	mapper := NewMemoryStoreMapper(d.mapperDB)
	exists, err := mapper.Exists(ctx, workspaceUUID, memoryStoreExternalID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func ensureMemoryPathAvailable(
	ctx context.Context,
	mapper MemoryMapper,
	workspaceUUID string,
	storeUUID string,
	path string,
	excludeMemoryUUID string,
) error {
	existingID, found, err := mapper.FindPathConflict(ctx, workspaceUUID, storeUUID, path, excludeMemoryUUID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
}

func memoryPathConflict(ctx context.Context, mapper MemoryMapper, workspaceUUID, storeUUID, path string) error {
	existingID, found, err := mapper.FindPathConflict(ctx, workspaceUUID, storeUUID, path, "")
	if err == nil && found {
		return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
	}
	return ErrDuplicate
}

func insertMemoryVersion(ctx context.Context, mapper MemoryVersionMapper, version MemoryVersion) (MemoryVersion, error) {
	row, err := mapper.Insert(ctx, insertMemoryVersionParams{
		UUID:                      version.UUID,
		ExternalID:                version.ExternalID,
		OrganizationUUID:          version.OrganizationUUID,
		WorkspaceUUID:             version.WorkspaceUUID,
		MemoryStoreUUID:           version.MemoryStoreUUID,
		MemoryStoreExternalID:     version.MemoryStoreExternalID,
		MemoryUUID:                version.MemoryUUID,
		MemoryExternalID:          version.MemoryExternalID,
		Operation:                 version.Operation,
		Path:                      version.Path,
		ContentSizeBytes:          version.ContentSizeBytes,
		ContentSHA256:             version.ContentSHA256,
		S3Bucket:                  version.S3Bucket,
		S3Key:                     version.S3Key,
		CreatedByActorType:        version.CreatedBy.Type,
		CreatedByAPIKeyUUID:       optionalMemoryString(version.CreatedBy.APIKeyUUID),
		CreatedByAPIKeyExternalID: optionalMemoryString(version.CreatedBy.APIKeyExternalID),
		CreatedBySessionID:        optionalMemoryString(version.CreatedBy.SessionID),
		CreatedByUserID:           optionalMemoryString(version.CreatedBy.UserID),
		CreatedAt:                 version.CreatedAt,
	})
	return memoryVersionFromMapperRow(row, err)
}

func memoryStoreFromMapperRow(row memoryStoreRow, err error) (MemoryStore, error) {
	if err != nil {
		return MemoryStore{}, mapNoRows(err)
	}
	return row.store(), nil
}

func memoryFromMapperRow(row memoryRow, err error) (Memory, error) {
	if err != nil {
		return Memory{}, mapNoRows(err)
	}
	return row.memory(), nil
}

func memoryVersionFromMapperRow(row memoryVersionRow, err error) (MemoryVersion, error) {
	if err != nil {
		return MemoryVersion{}, mapNoRows(err)
	}
	return row.version(), nil
}

func memoryStoresFromMapperRows(rows []memoryStoreRow) []MemoryStore {
	stores := make([]MemoryStore, len(rows))
	for index := range rows {
		stores[index] = rows[index].store()
	}
	return stores
}

func memoriesFromMapperRows(rows []memoryRow) []Memory {
	memories := make([]Memory, len(rows))
	for index := range rows {
		memories[index] = rows[index].memory()
	}
	return memories
}

func memoryVersionsFromMapperRows(rows []memoryVersionRow) []MemoryVersion {
	versions := make([]MemoryVersion, len(rows))
	for index := range rows {
		versions[index] = rows[index].version()
	}
	return versions
}

func memoryObjectRefsFromMapperRows(rows []memoryObjectRefRow) []ObjectRef {
	refs := make([]ObjectRef, len(rows))
	for index := range rows {
		refs[index] = ObjectRef{
			WorkspaceUUID: rows[index].WorkspaceUUID,
			Bucket:        rows[index].Bucket,
			Key:           rows[index].Key,
			ResourceType:  "memory_version",
			ResourceID:    rows[index].ResourceID,
		}
	}
	return refs
}

func (r memoryStoreRow) store() MemoryStore {
	return MemoryStore{
		UUID:                r.UUID,
		ExternalID:          r.ExternalID,
		OrganizationUUID:    r.OrganizationUUID,
		WorkspaceUUID:       r.WorkspaceUUID,
		CreatedByAPIKeyUUID: r.CreatedByAPIKeyUUID,
		Name:                r.Name,
		Description:         r.Description,
		Metadata:            bytes.Clone(r.Metadata),
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
		ArchivedAt:          r.ArchivedAt,
		DeletedAt:           r.DeletedAt,
	}
}

func (r memoryRow) memory() Memory {
	return Memory{
		UUID:                     r.UUID,
		ExternalID:               r.ExternalID,
		OrganizationUUID:         r.OrganizationUUID,
		WorkspaceUUID:            r.WorkspaceUUID,
		MemoryStoreUUID:          r.MemoryStoreUUID,
		MemoryStoreExternalID:    r.MemoryStoreExternalID,
		CurrentVersionUUID:       memoryStringValue(r.CurrentVersionUUID),
		CurrentVersionExternalID: r.CurrentVersionExternalID,
		Path:                     r.Path,
		ContentSizeBytes:         r.ContentSizeBytes,
		ContentSHA256:            r.ContentSHA256,
		S3Bucket:                 r.S3Bucket,
		S3Key:                    r.S3Key,
		CreatedAt:                r.CreatedAt,
		UpdatedAt:                r.UpdatedAt,
		DeletedAt:                r.DeletedAt,
	}
}

func (r memoryVersionRow) version() MemoryVersion {
	version := MemoryVersion{
		UUID:                  r.UUID,
		ExternalID:            r.ExternalID,
		OrganizationUUID:      r.OrganizationUUID,
		WorkspaceUUID:         r.WorkspaceUUID,
		MemoryStoreUUID:       r.MemoryStoreUUID,
		MemoryStoreExternalID: r.MemoryStoreExternalID,
		MemoryUUID:            r.MemoryUUID,
		MemoryExternalID:      r.MemoryExternalID,
		Operation:             r.Operation,
		Path:                  r.Path,
		ContentSizeBytes:      r.ContentSizeBytes,
		ContentSHA256:         r.ContentSHA256,
		S3Bucket:              r.S3Bucket,
		S3Key:                 r.S3Key,
		CreatedBy: MemoryActor{
			Type:             r.CreatedByActorType,
			APIKeyUUID:       memoryStringValue(r.CreatedByAPIKeyUUID),
			APIKeyExternalID: memoryStringValue(r.CreatedByAPIKeyExternalID),
			SessionID:        memoryStringValue(r.CreatedBySessionID),
			UserID:           memoryStringValue(r.CreatedByUserID),
		},
		RedactedAt: r.RedactedAt,
		CreatedAt:  r.CreatedAt,
	}
	if r.RedactedByActorType != nil {
		version.RedactedBy = &MemoryActor{
			Type:             *r.RedactedByActorType,
			APIKeyUUID:       memoryStringValue(r.RedactedByAPIKeyUUID),
			APIKeyExternalID: memoryStringValue(r.RedactedByAPIKeyExternalID),
			SessionID:        memoryStringValue(r.RedactedBySessionID),
			UserID:           memoryStringValue(r.RedactedByUserID),
		}
	}
	return version
}

func memoryJSONArg(raw json.RawMessage) []byte {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func optionalMemoryString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func memoryStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
