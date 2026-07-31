package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	return getMemoryStoreSQLX(ctx, d.sql, `
		insert into memory_stores (
			uuid, external_id, organization_uuid, workspace_uuid, created_by_api_key_uuid,
			name, description, metadata, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :created_by_api_key_uuid,
			:name, :description, CAST(:metadata AS jsonb), :created_at, :created_at
		)
		returning `+memoryStoreColumns()+`
	`, map[string]any{
		"uuid":                    dbUUID(store.UUID),
		"external_id":             store.ExternalID,
		"organization_uuid":       dbUUID(store.OrganizationUUID),
		"workspace_uuid":          dbUUID(store.WorkspaceUUID),
		"created_by_api_key_uuid": dbUUID(store.CreatedByAPIKeyUUID),
		"name":                    store.Name,
		"description":             store.Description,
		"metadata":                jsonArg(store.Metadata),
		"created_at":              store.CreatedAt,
	})
}

func (d *DB) GetMemoryStore(ctx context.Context, workspaceUUID, externalID string) (MemoryStore, error) {
	return getMemoryStoreSQLX(ctx, d.sql, memoryStoreSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
	`, memoryStoreLookupArguments(workspaceUUID, externalID))
}

func (d *DB) GetMemoryStoreByExternalID(ctx context.Context, externalID string) (MemoryStore, error) {
	return getMemoryStoreSQLX(ctx, d.sql, memoryStoreSelectSQL()+`
		where external_id = :external_id and deleted_at is null
	`, map[string]any{"external_id": externalID})
}

func (d *DB) UpdateMemoryStore(ctx context.Context, workspaceUUID, externalID string, next MemoryStore) (MemoryStore, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return MemoryStore{}, err
	}
	defer tx.Rollback()

	current, err := getMemoryStoreSQLX(ctx, tx, memoryStoreSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, memoryStoreLookupArguments(workspaceUUID, externalID))
	if err != nil {
		return MemoryStore{}, err
	}
	if current.ArchivedAt != nil {
		return MemoryStore{}, ErrInvalidState
	}
	updated, err := getMemoryStoreSQLX(ctx, tx, `
		update memory_stores
		set name = :name,
			description = :description,
			metadata = CAST(:metadata AS jsonb),
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+memoryStoreColumns()+`
	`, map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    externalID,
		"name":           next.Name,
		"description":    next.Description,
		"metadata":       jsonArg(next.Metadata),
		"updated_at":     next.UpdatedAt,
	})
	if err != nil {
		return MemoryStore{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryStore{}, err
	}
	return updated, nil
}

func (d *DB) ArchiveMemoryStore(ctx context.Context, workspaceUUID, externalID string) (MemoryStore, error) {
	return getMemoryStoreSQLX(ctx, d.sql, `
		update memory_stores
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		returning `+memoryStoreColumns()+`
	`, memoryStoreLookupArguments(workspaceUUID, externalID))
}

func (d *DB) DeleteMemoryStore(ctx context.Context, workspaceUUID, externalID string) ([]ObjectRef, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var storeUUID uuid.UUID
	arguments := memoryStoreLookupArguments(workspaceUUID, externalID)
	if err := namedGetContext(ctx, tx, &storeUUID, `
		select uuid
		from memory_stores
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, arguments); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}

	var refRows []objectRefRow
	arguments["store_uuid"] = storeUUID
	if err := namedSelectContext(ctx, tx, &refRows, `
		select workspace_uuid, coalesce(s3_bucket, '') AS bucket,
			coalesce(s3_key, '') AS key, external_id AS resource_id
		from memory_versions
		where workspace_uuid = :workspace_uuid
			and memory_store_uuid = :store_uuid
			and s3_key is not null
	`, arguments); err != nil {
		return nil, err
	}
	refs := make([]ObjectRef, len(refRows))
	for index := range refRows {
		refs[index] = ObjectRef{
			WorkspaceUUID: refRows[index].WorkspaceUUID.String(),
			Bucket:        refRows[index].Bucket,
			Key:           refRows[index].Key,
			ResourceType:  "memory_version",
			ResourceID:    refRows[index].ResourceID,
		}
	}

	if _, err := namedExecContext(ctx, tx, `
		delete from memory_versions
		where workspace_uuid = :workspace_uuid and memory_store_uuid = :store_uuid
	`, arguments); err != nil {
		return nil, err
	}
	if _, err := namedExecContext(ctx, tx, `
		delete from memories
		where workspace_uuid = :workspace_uuid and memory_store_uuid = :store_uuid
	`, arguments); err != nil {
		return nil, err
	}
	if _, err := namedExecContext(ctx, tx, `
		delete from memory_stores
		where workspace_uuid = :workspace_uuid
			and uuid = :store_uuid
	`, arguments); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return refs, nil
}

func (d *DB) ListMemoryStoresPage(ctx context.Context, params ListMemoryStoresPageParams) ([]MemoryStore, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := memoryStoreSelectSQL() + `
		where workspace_uuid = :workspace_uuid and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid": dbUUID(params.WorkspaceUUID),
		"limit":          params.Limit + 1,
	}
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.CreatedAtGTE != nil {
		query += " and created_at >= :created_at_gte"
		arguments["created_at_gte"] = *params.CreatedAtGTE
	}
	if params.CreatedAtLTE != nil {
		query += " and created_at <= :created_at_lte"
		arguments["created_at_lte"] = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < :cursor_uuid))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	query += " order by created_at desc, uuid desc limit :limit"

	stores, err := selectMemoryStoresSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(stores) > params.Limit
	if hasMore {
		stores = stores[:params.Limit]
	}
	return stores, hasMore, nil
}

func (d *DB) CreateMemory(ctx context.Context, memory Memory, version MemoryVersion) (Memory, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback()

	store, err := getMemoryStoreSQLX(ctx, tx, memoryStoreSelectSQL()+`
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
		for update
	`, memoryStoreLookupArguments(memory.WorkspaceUUID, memory.MemoryStoreExternalID))
	if err != nil {
		return Memory{}, err
	}
	if store.ArchivedAt != nil {
		return Memory{}, ErrInvalidState
	}
	if err := d.ensureMemoryPathAvailable(ctx, tx, store.UUID, memory.Path, ""); err != nil {
		return Memory{}, err
	}

	created, err := getMemorySQLX(ctx, tx, `
		insert into memories (
			uuid, external_id, organization_uuid, workspace_uuid, memory_store_uuid,
			memory_store_external_id, current_version_external_id, path,
			content_size_bytes, content_sha256, s3_bucket, s3_key, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :memory_store_uuid,
			:memory_store_external_id, :current_version_external_id, :path,
			:content_size_bytes, :content_sha256, :s3_bucket, :s3_key, :created_at, :created_at
		)
		returning `+memoryColumns()+`
	`, map[string]any{
		"uuid":                        dbUUID(memory.UUID),
		"external_id":                 memory.ExternalID,
		"organization_uuid":           dbUUID(store.OrganizationUUID),
		"workspace_uuid":              dbUUID(store.WorkspaceUUID),
		"memory_store_uuid":           dbUUID(store.UUID),
		"memory_store_external_id":    store.ExternalID,
		"current_version_external_id": version.ExternalID,
		"path":                        memory.Path,
		"content_size_bytes":          memory.ContentSizeBytes,
		"content_sha256":              memory.ContentSHA256,
		"s3_bucket":                   memory.S3Bucket,
		"s3_key":                      memory.S3Key,
		"created_at":                  memory.CreatedAt,
	})
	if isUniqueViolation(err) {
		return Memory{}, d.memoryPathConflict(ctx, tx, store.UUID, memory.Path)
	}
	if err != nil {
		return Memory{}, err
	}

	version.OrganizationUUID = store.OrganizationUUID
	version.WorkspaceUUID = store.WorkspaceUUID
	version.MemoryStoreUUID = store.UUID
	version.MemoryStoreExternalID = store.ExternalID
	version.MemoryUUID = created.UUID
	version.MemoryExternalID = created.ExternalID
	insertedVersion, err := insertMemoryVersion(ctx, tx, version)
	if err != nil {
		return Memory{}, err
	}
	updated, err := getMemorySQLX(ctx, tx, `
		update memories
		set current_version_uuid = :version_uuid,
			current_version_external_id = :version_external_id
		where workspace_uuid = :workspace_uuid
			and uuid = :memory_uuid
		returning `+memoryColumns()+`
	`, map[string]any{
		"workspace_uuid":      dbUUID(store.WorkspaceUUID),
		"memory_uuid":         dbUUID(created.UUID),
		"version_uuid":        dbUUID(insertedVersion.UUID),
		"version_external_id": insertedVersion.ExternalID,
	})
	if err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, err
	}
	return updated, nil
}

func (d *DB) GetMemory(ctx context.Context, workspaceUUID, memoryStoreExternalID, memoryExternalID string) (Memory, error) {
	return getMemorySQLX(ctx, d.sql, memorySelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and external_id = :memory_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":           dbUUID(workspaceUUID),
		"memory_store_external_id": memoryStoreExternalID,
		"memory_external_id":       memoryExternalID,
	})
}

func (d *DB) UpdateMemory(ctx context.Context, input UpdateMemoryInput) (MemoryMutationResult, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return MemoryMutationResult{}, err
	}
	defer tx.Rollback()

	current, storeArchived, err := d.getActiveMemoryForMutation(ctx, tx, input.WorkspaceUUID, input.MemoryStoreExternalID, input.MemoryExternalID)
	if err != nil {
		return MemoryMutationResult{}, err
	}
	if storeArchived {
		return MemoryMutationResult{}, ErrInvalidState
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
			if err := tx.Commit(); err != nil {
				return MemoryMutationResult{}, err
			}
			return MemoryMutationResult{Memory: current}, nil
		}
		return MemoryMutationResult{}, ErrPreconditionFailed
	}
	if input.BaseVersionExternalID != "" && current.CurrentVersionExternalID != input.BaseVersionExternalID {
		return MemoryMutationResult{}, ErrVersionConflict
	}
	if current.Path == targetPath && current.ContentSHA256 == targetSHA {
		if err := tx.Commit(); err != nil {
			return MemoryMutationResult{}, err
		}
		return MemoryMutationResult{Memory: current}, nil
	}
	if targetPath != current.Path {
		if err := d.ensureMemoryPathAvailable(ctx, tx, current.MemoryStoreUUID, targetPath, current.UUID); err != nil {
			return MemoryMutationResult{}, err
		}
	}

	pathValue := targetPath
	contentSize := targetSize
	contentSHA := targetSHA
	bucket := targetBucket
	key := targetKey
	version, err := insertMemoryVersion(ctx, tx, MemoryVersion{
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
	if err != nil {
		return MemoryMutationResult{}, err
	}
	updated, err := getMemorySQLX(ctx, tx, `
		update memories
		set current_version_uuid = :version_uuid,
			current_version_external_id = :version_external_id,
			path = :path,
			content_size_bytes = :content_size_bytes,
			content_sha256 = :content_sha256,
			s3_bucket = :s3_bucket,
			s3_key = :s3_key,
			updated_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and external_id = :memory_external_id
			and deleted_at is null
		returning `+memoryColumns()+`
	`, map[string]any{
		"workspace_uuid":           dbUUID(input.WorkspaceUUID),
		"memory_store_external_id": input.MemoryStoreExternalID,
		"memory_external_id":       input.MemoryExternalID,
		"version_uuid":             dbUUID(version.UUID),
		"version_external_id":      version.ExternalID,
		"path":                     targetPath,
		"content_size_bytes":       targetSize,
		"content_sha256":           targetSHA,
		"s3_bucket":                targetBucket,
		"s3_key":                   targetKey,
		"updated_at":               input.Now,
	})
	if isUniqueViolation(err) {
		return MemoryMutationResult{}, d.memoryPathConflict(ctx, tx, current.MemoryStoreUUID, targetPath)
	}
	if err != nil {
		return MemoryMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryMutationResult{}, err
	}
	return MemoryMutationResult{Memory: updated, VersionCreated: true}, nil
}

func (d *DB) DeleteMemory(ctx context.Context, input DeleteMemoryInput) error {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, storeArchived, err := d.getActiveMemoryForMutation(ctx, tx, input.WorkspaceUUID, input.MemoryStoreExternalID, input.MemoryExternalID)
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
	version, err := insertMemoryVersion(ctx, tx, MemoryVersion{
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
	rowsAffected, err := namedExecRowsAffected(ctx, tx, `
		update memories
		set current_version_uuid = :version_uuid,
			current_version_external_id = :version_external_id,
			updated_at = :updated_at,
			deleted_at = :updated_at
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and external_id = :memory_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_uuid":           dbUUID(input.WorkspaceUUID),
		"memory_store_external_id": input.MemoryStoreExternalID,
		"memory_external_id":       input.MemoryExternalID,
		"version_uuid":             dbUUID(version.UUID),
		"version_external_id":      version.ExternalID,
		"updated_at":               input.Now,
	})
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return tx.Commit()
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

	query := memorySelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid":           dbUUID(params.WorkspaceUUID),
		"memory_store_external_id": params.MemoryStoreExternalID,
		"limit":                    params.Limit + 1,
	}
	if params.PathPrefix != "" {
		query += " and left(path, length(:path_prefix)) = :path_prefix"
		arguments["path_prefix"] = params.PathPrefix
	}
	if params.Cursor != nil {
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
		switch orderBy {
		case "path":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (path %s :cursor_path or (path = :cursor_path and uuid %s :cursor_uuid))", op, op)
			arguments["cursor_path"] = params.Cursor.Path
		case "created_at":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (created_at %s :cursor_created_at or (created_at = :cursor_created_at and uuid %s :cursor_uuid))", op, op)
			arguments["cursor_created_at"] = params.Cursor.CreatedAt
		case "updated_at":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (updated_at %s :cursor_updated_at or (updated_at = :cursor_updated_at and uuid %s :cursor_uuid))", op, op)
			arguments["cursor_updated_at"] = params.Cursor.UpdatedAt
		}
	}
	query += fmt.Sprintf(" order by %s %s, uuid %s limit :limit", orderBy, order, order)

	memories, err := selectMemoriesSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
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
	query := memorySelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_uuid":           dbUUID(params.WorkspaceUUID),
		"memory_store_external_id": params.MemoryStoreExternalID,
	}
	if params.PathPrefix != "" {
		query += " and left(path, length(:path_prefix)) = :path_prefix"
		arguments["path_prefix"] = params.PathPrefix
	}
	query += " order by path asc, uuid asc"
	return selectMemoriesSQLX(ctx, d.sql, query, arguments)
}

func (d *DB) GetMemoryVersion(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionExternalID string) (MemoryVersion, error) {
	return getMemoryVersionSQLX(ctx, d.sql, memoryVersionSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and external_id = :version_external_id
	`, map[string]any{
		"workspace_uuid":           dbUUID(workspaceUUID),
		"memory_store_external_id": memoryStoreExternalID,
		"version_external_id":      versionExternalID,
	})
}

func (d *DB) ListMemoryVersionsPage(ctx context.Context, params ListMemoryVersionsPageParams) ([]MemoryVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceUUID, params.MemoryStoreExternalID); err != nil {
		return nil, false, err
	}
	query := memoryVersionSelectSQL() + `
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
	`
	arguments := map[string]any{
		"workspace_uuid":           dbUUID(params.WorkspaceUUID),
		"memory_store_external_id": params.MemoryStoreExternalID,
		"limit":                    params.Limit + 1,
	}
	if params.MemoryExternalID != "" {
		query += " and memory_external_id = :memory_external_id"
		arguments["memory_external_id"] = params.MemoryExternalID
	}
	if params.Operation != "" {
		query += " and operation = :operation"
		arguments["operation"] = params.Operation
	}
	if params.APIKeyExternalID != "" {
		query += " and created_by_api_key_external_id = :api_key_external_id"
		arguments["api_key_external_id"] = params.APIKeyExternalID
	}
	if params.SessionID != "" {
		query += " and created_by_session_id = :session_id"
		arguments["session_id"] = params.SessionID
	}
	if params.CreatedAtGTE != nil {
		query += " and created_at >= :created_at_gte"
		arguments["created_at_gte"] = *params.CreatedAtGTE
	}
	if params.CreatedAtLTE != nil {
		query += " and created_at <= :created_at_lte"
		arguments["created_at_lte"] = *params.CreatedAtLTE
	}
	if params.Cursor != nil {
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and uuid < :cursor_uuid))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_uuid"] = dbUUID(params.Cursor.UUID)
	}
	query += " order by created_at desc, uuid desc limit :limit"

	versions, err := selectMemoryVersionsSQLX(ctx, d.sql, query, arguments)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(versions) > params.Limit
	if hasMore {
		versions = versions[:params.Limit]
	}
	return versions, hasMore, nil
}

func (d *DB) RedactMemoryVersion(ctx context.Context, workspaceUUID, memoryStoreExternalID, versionExternalID string, actor MemoryActor, now time.Time) (MemoryVersion, *ObjectRef, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return MemoryVersion{}, nil, err
	}
	defer tx.Rollback()

	arguments := map[string]any{
		"workspace_uuid":           dbUUID(workspaceUUID),
		"memory_store_external_id": memoryStoreExternalID,
		"version_external_id":      versionExternalID,
	}
	version, err := getMemoryVersionSQLX(ctx, tx, memoryVersionSelectSQL()+`
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and external_id = :version_external_id
		for update
	`, arguments)
	if err != nil {
		return MemoryVersion{}, nil, err
	}

	var activeHead int
	arguments["version_uuid"] = dbUUID(version.UUID)
	if err := namedGetContext(ctx, tx, &activeHead, `
		select CAST(count(*) AS integer)
		from memories
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and current_version_uuid = :version_uuid
			and deleted_at is null
	`, arguments); err != nil {
		return MemoryVersion{}, nil, err
	}
	if activeHead > 0 {
		return MemoryVersion{}, nil, ErrInvalidState
	}
	if version.RedactedAt != nil {
		if err := tx.Commit(); err != nil {
			return MemoryVersion{}, nil, err
		}
		return version, nil, nil
	}

	var ref *ObjectRef
	if version.S3Key != nil && *version.S3Key != "" {
		ref = &ObjectRef{
			WorkspaceUUID: version.WorkspaceUUID,
			ResourceType:  "memory_version",
			ResourceID:    version.ExternalID,
		}
		if version.S3Bucket != nil {
			ref.Bucket = *version.S3Bucket
		}
		ref.Key = *version.S3Key
	}
	arguments["now"] = now
	arguments["actor_type"] = actor.Type
	arguments["api_key_uuid"] = dbNullableUUID(&actor.APIKeyUUID)
	arguments["api_key_external_id"] = nullableString(actor.APIKeyExternalID)
	arguments["session_id"] = nullableString(actor.SessionID)
	arguments["user_id"] = nullableString(actor.UserID)
	updated, err := getMemoryVersionSQLX(ctx, tx, `
		update memory_versions
		set path = null,
			content_size_bytes = null,
			content_sha256 = null,
			s3_bucket = null,
			s3_key = null,
			redacted_at = :now,
			redacted_by_actor_type = :actor_type,
			redacted_by_api_key_uuid = :api_key_uuid,
			redacted_by_api_key_external_id = :api_key_external_id,
			redacted_by_session_id = :session_id,
			redacted_by_user_id = :user_id
		where workspace_uuid = :workspace_uuid
			and memory_store_external_id = :memory_store_external_id
			and external_id = :version_external_id
		returning `+memoryVersionColumns()+`
	`, arguments)
	if err != nil {
		return MemoryVersion{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryVersion{}, nil, err
	}
	return updated, ref, nil
}

func (d *DB) getActiveMemoryForMutation(ctx context.Context, tx sqlxNamedQueryer, workspaceUUID, memoryStoreExternalID, memoryExternalID string) (Memory, bool, error) {
	var row activeMemoryRow
	err := namedGetContext(ctx, tx, &row, `
		select m.uuid, m.external_id,
			m.organization_uuid,
			m.workspace_uuid,
			m.memory_store_uuid, m.memory_store_external_id,
			m.current_version_uuid,
			coalesce(m.current_version_external_id, '') as current_version_external_id,
			m.path, m.content_size_bytes,
			m.content_sha256, m.s3_bucket, m.s3_key, m.created_at, m.updated_at,
			m.deleted_at, ms.archived_at AS archived_at
		from memories m
		join memory_stores ms on ms.uuid = m.memory_store_uuid
		where m.workspace_uuid = :workspace_uuid
			and m.memory_store_external_id = :memory_store_external_id
			and m.external_id = :memory_external_id
			and m.deleted_at is null
			and ms.deleted_at is null
		for update of m, ms
	`, map[string]any{
		"workspace_uuid":           dbUUID(workspaceUUID),
		"memory_store_external_id": memoryStoreExternalID,
		"memory_external_id":       memoryExternalID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, false, ErrNotFound
	}
	if err != nil {
		return Memory{}, false, err
	}
	return row.memory(), row.ArchivedAt != nil, nil
}

func (d *DB) ensureMemoryStoreExists(ctx context.Context, workspaceUUID, memoryStoreExternalID string) error {
	var storeUUID uuid.UUID
	if err := namedGetContext(ctx, d.sql, &storeUUID, `
		select uuid
		from memory_stores
		where workspace_uuid = :workspace_uuid and external_id = :external_id and deleted_at is null
	`, memoryStoreLookupArguments(workspaceUUID, memoryStoreExternalID)); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

func (d *DB) ensureMemoryPathAvailable(ctx context.Context, tx sqlxNamedQueryer, storeUUID, path, excludeMemoryUUID string) error {
	var existingID string
	query := `
		select external_id
		from memories
		where memory_store_uuid = :store_uuid
			and path = :path
	`
	arguments := map[string]any{
		"store_uuid": dbUUID(storeUUID),
		"path":       path,
	}
	if excludeMemoryUUID != "" {
		query += " and uuid <> :exclude_memory_uuid"
		arguments["exclude_memory_uuid"] = dbUUID(excludeMemoryUUID)
	}
	query += " and deleted_at is null"
	if err := namedGetContext(ctx, tx, &existingID, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
}

func (d *DB) memoryPathConflict(ctx context.Context, database sqlxNamedQueryer, storeUUID, path string) error {
	var existingID string
	if err := namedGetContext(ctx, database, &existingID, `
		select external_id
		from memories
		where memory_store_uuid = :store_uuid
			and path = :path
			and deleted_at is null
		limit 1
	`, map[string]any{
		"store_uuid": dbUUID(storeUUID),
		"path":       path,
	}); err == nil {
		return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
	}
	return ErrDuplicate
}

const insertMemoryVersionQuery = `
		insert into memory_versions (
			uuid, external_id, organization_uuid, workspace_uuid, memory_store_uuid,
			memory_store_external_id, memory_uuid, memory_external_id, operation, path,
			content_size_bytes, content_sha256, s3_bucket, s3_key, created_by_actor_type,
			created_by_api_key_uuid, created_by_api_key_external_id, created_by_session_id,
			created_by_user_id, created_at
		)
		values (
			:uuid, :external_id, :organization_uuid, :workspace_uuid, :memory_store_uuid,
			:memory_store_external_id, :memory_uuid, :memory_external_id, :operation, :path,
			:content_size_bytes, :content_sha256, :s3_bucket, :s3_key, :created_by_actor_type,
			:created_by_api_key_uuid, :created_by_api_key_external_id, :created_by_session_id,
			:created_by_user_id, :created_at
		)
		returning ` + `uuid, external_id,
			organization_uuid,
			workspace_uuid,
			memory_store_uuid,
			memory_store_external_id, memory_uuid, memory_external_id,
			operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
			created_by_actor_type,
			created_by_api_key_uuid,
			created_by_api_key_external_id,
			created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
			redacted_by_api_key_uuid,
			redacted_by_api_key_external_id, redacted_by_session_id,
			redacted_by_user_id, created_at`

func insertMemoryVersion(ctx context.Context, tx sqlxNamedQueryer, version MemoryVersion) (MemoryVersion, error) {
	return getMemoryVersionSQLX(ctx, tx, insertMemoryVersionQuery, map[string]any{
		"uuid":                           dbUUID(version.UUID),
		"external_id":                    version.ExternalID,
		"organization_uuid":              dbUUID(version.OrganizationUUID),
		"workspace_uuid":                 dbUUID(version.WorkspaceUUID),
		"memory_store_uuid":              dbUUID(version.MemoryStoreUUID),
		"memory_store_external_id":       version.MemoryStoreExternalID,
		"memory_uuid":                    dbUUID(version.MemoryUUID),
		"memory_external_id":             version.MemoryExternalID,
		"operation":                      version.Operation,
		"path":                           nullableStringPtr(version.Path),
		"content_size_bytes":             nullableInt64Ptr(version.ContentSizeBytes),
		"content_sha256":                 nullableStringPtr(version.ContentSHA256),
		"s3_bucket":                      nullableStringPtr(version.S3Bucket),
		"s3_key":                         nullableStringPtr(version.S3Key),
		"created_by_actor_type":          version.CreatedBy.Type,
		"created_by_api_key_uuid":        dbNullableUUID(&version.CreatedBy.APIKeyUUID),
		"created_by_api_key_external_id": nullableString(version.CreatedBy.APIKeyExternalID),
		"created_by_session_id":          nullableString(version.CreatedBy.SessionID),
		"created_by_user_id":             nullableString(version.CreatedBy.UserID),
		"created_at":                     version.CreatedAt,
	})
}

func memoryStoreColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		created_by_api_key_uuid,
		name, description, metadata, created_at, updated_at,
		archived_at, deleted_at`
}

func memoryStoreSelectSQL() string {
	return `select ` + memoryStoreColumns() + ` from memory_stores`
}

func memoryColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		memory_store_uuid, memory_store_external_id,
		current_version_uuid,
		coalesce(current_version_external_id, '') as current_version_external_id,
		path, content_size_bytes, content_sha256, s3_bucket, s3_key,
		created_at, updated_at, deleted_at`
}

func memorySelectSQL() string {
	return `select ` + memoryColumns() + ` from memories`
}

func memoryVersionColumns() string {
	return `uuid, external_id,
		organization_uuid,
		workspace_uuid,
		memory_store_uuid,
		memory_store_external_id, memory_uuid, memory_external_id,
		operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
		created_by_actor_type, created_by_api_key_uuid,
		created_by_api_key_external_id,
		created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
		redacted_by_api_key_uuid,
		redacted_by_api_key_external_id, redacted_by_session_id,
		redacted_by_user_id, created_at`
}

func memoryVersionSelectSQL() string {
	return `select ` + memoryVersionColumns() + ` from memory_versions`
}

func memoryStoreLookupArguments(workspaceUUID, externalID string) map[string]any {
	return map[string]any{
		"workspace_uuid": dbUUID(workspaceUUID),
		"external_id":    externalID,
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Ptr(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
