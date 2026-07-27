package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type MemoryStore struct {
	ID                int64
	UUID              string
	ExternalID        string
	OrganizationID    int64
	WorkspaceID       int64
	CreatedByAPIKeyID int64
	Name              string
	Description       string
	Metadata          json.RawMessage
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ArchivedAt        *time.Time
	DeletedAt         *time.Time
}

type Memory struct {
	ID                       int64
	UUID                     string
	ExternalID               string
	OrganizationID           int64
	WorkspaceID              int64
	MemoryStoreID            int64
	MemoryStoreExternalID    string
	CurrentVersionID         int64
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
	APIKeyID         int64
	APIKeyExternalID string
	SessionID        string
	UserID           string
}

type MemoryVersion struct {
	ID                    int64
	UUID                  string
	ExternalID            string
	OrganizationID        int64
	WorkspaceID           int64
	MemoryStoreID         int64
	MemoryStoreExternalID string
	MemoryID              int64
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
	WorkspaceID  int64
	Bucket       string
	Key          string
	ResourceType string
	ResourceID   string
}

type MemoryStorePageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type MemoryPageCursor struct {
	Path      string
	CreatedAt time.Time
	UpdatedAt time.Time
	ID        int64
}

type MemoryVersionPageCursor struct {
	CreatedAt time.Time
	ID        int64
}

type ListMemoryStoresPageParams struct {
	WorkspaceID     int64
	Limit           int
	Cursor          *MemoryStorePageCursor
	IncludeArchived bool
	CreatedAtGTE    *time.Time
	CreatedAtLTE    *time.Time
}

type ListMemoriesPageParams struct {
	WorkspaceID           int64
	MemoryStoreExternalID string
	Limit                 int
	Cursor                *MemoryPageCursor
	PathPrefix            string
	Order                 string
	OrderBy               string
}

type ListMemoryVersionsPageParams struct {
	WorkspaceID           int64
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
	WorkspaceID           int64
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
	WorkspaceID           int64
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
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, metadata, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_id, :workspace_id, :created_by_api_key_id,
			:name, :description, CAST(:metadata AS jsonb), :created_at, :created_at
		)
		returning `+memoryStoreColumns()+`
	`, map[string]any{
		"uuid":                  store.UUID,
		"external_id":           store.ExternalID,
		"organization_id":       store.OrganizationID,
		"workspace_id":          store.WorkspaceID,
		"created_by_api_key_id": store.CreatedByAPIKeyID,
		"name":                  store.Name,
		"description":           store.Description,
		"metadata":              jsonArg(store.Metadata),
		"created_at":            store.CreatedAt,
	})
}

func (d *DB) GetMemoryStore(ctx context.Context, workspaceID int64, externalID string) (MemoryStore, error) {
	return getMemoryStoreSQLX(ctx, d.sql, memoryStoreSelectSQL()+`
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
	`, map[string]any{"workspace_id": workspaceID, "external_id": externalID})
}

func (d *DB) GetMemoryStoreByExternalID(ctx context.Context, externalID string) (MemoryStore, error) {
	return getMemoryStoreSQLX(ctx, d.sql, memoryStoreSelectSQL()+`
		where external_id = :external_id and deleted_at is null
	`, map[string]any{"external_id": externalID})
}

func (d *DB) UpdateMemoryStore(ctx context.Context, workspaceID int64, externalID string, next MemoryStore) (MemoryStore, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return MemoryStore{}, err
	}
	defer tx.Rollback()

	current, err := getMemoryStoreSQLX(ctx, tx, memoryStoreSelectSQL()+`
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"workspace_id": workspaceID, "external_id": externalID})
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
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
		returning `+memoryStoreColumns()+`
	`, map[string]any{
		"workspace_id": workspaceID,
		"external_id":  externalID,
		"name":         next.Name,
		"description":  next.Description,
		"metadata":     jsonArg(next.Metadata),
		"updated_at":   next.UpdatedAt,
	})
	if err != nil {
		return MemoryStore{}, err
	}
	if err := tx.Commit(); err != nil {
		return MemoryStore{}, err
	}
	return updated, nil
}

func (d *DB) ArchiveMemoryStore(ctx context.Context, workspaceID int64, externalID string) (MemoryStore, error) {
	return getMemoryStoreSQLX(ctx, d.sql, `
		update memory_stores
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
		returning `+memoryStoreColumns()+`
	`, map[string]any{"workspace_id": workspaceID, "external_id": externalID})
}

func (d *DB) DeleteMemoryStore(ctx context.Context, workspaceID int64, externalID string) ([]ObjectRef, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var storeID int64
	arguments := map[string]any{"workspace_id": workspaceID, "external_id": externalID}
	if err := namedGetContext(ctx, tx, &storeID, `
		select id
		from memory_stores
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
		for update
	`, arguments); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}

	var refRows []objectRefRow
	arguments["store_id"] = storeID
	if err := namedSelectContext(ctx, tx, &refRows, `
		select workspace_id, coalesce(s3_bucket, '') AS bucket,
			coalesce(s3_key, '') AS key, external_id AS resource_id
		from memory_versions
		where workspace_id = :workspace_id
			and memory_store_id = :store_id
			and s3_key is not null
	`, arguments); err != nil {
		return nil, err
	}
	refs := make([]ObjectRef, len(refRows))
	for index := range refRows {
		refs[index] = ObjectRef{
			WorkspaceID:  refRows[index].WorkspaceID,
			Bucket:       refRows[index].Bucket,
			Key:          refRows[index].Key,
			ResourceType: "memory_version",
			ResourceID:   refRows[index].ResourceID,
		}
	}

	if _, err := namedExecContext(ctx, tx, `
		delete from memory_versions
		where workspace_id = :workspace_id and memory_store_id = :store_id
	`, arguments); err != nil {
		return nil, err
	}
	if _, err := namedExecContext(ctx, tx, `
		delete from memories
		where workspace_id = :workspace_id and memory_store_id = :store_id
	`, arguments); err != nil {
		return nil, err
	}
	if _, err := namedExecContext(ctx, tx, `
		delete from memory_stores
		where workspace_id = :workspace_id and id = :store_id
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
		where workspace_id = :workspace_id and deleted_at is null
	`
	arguments := map[string]any{"workspace_id": params.WorkspaceID, "limit": params.Limit + 1}
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
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and id < :cursor_id))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_id"] = params.Cursor.ID
	}
	query += " order by created_at desc, id desc limit :limit"

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
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
		for update
	`, map[string]any{"workspace_id": memory.WorkspaceID, "external_id": memory.MemoryStoreExternalID})
	if err != nil {
		return Memory{}, err
	}
	if store.ArchivedAt != nil {
		return Memory{}, ErrInvalidState
	}
	if err := d.ensureMemoryPathAvailable(ctx, tx, store.ID, memory.Path, 0); err != nil {
		return Memory{}, err
	}

	created, err := getMemorySQLX(ctx, tx, `
		insert into memories (
			uuid, external_id, organization_id, workspace_id, memory_store_id,
			memory_store_external_id, current_version_external_id, path,
			content_size_bytes, content_sha256, s3_bucket, s3_key, created_at, updated_at
		)
		values (
			:uuid, :external_id, :organization_id, :workspace_id, :memory_store_id,
			:memory_store_external_id, :current_version_external_id, :path,
			:content_size_bytes, :content_sha256, :s3_bucket, :s3_key, :created_at, :created_at
		)
		returning `+memoryColumns()+`
	`, map[string]any{
		"uuid":                        memory.UUID,
		"external_id":                 memory.ExternalID,
		"organization_id":             store.OrganizationID,
		"workspace_id":                store.WorkspaceID,
		"memory_store_id":             store.ID,
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
		return Memory{}, d.memoryPathConflict(ctx, tx, store.ID, memory.Path)
	}
	if err != nil {
		return Memory{}, err
	}

	version.OrganizationID = store.OrganizationID
	version.WorkspaceID = store.WorkspaceID
	version.MemoryStoreID = store.ID
	version.MemoryStoreExternalID = store.ExternalID
	version.MemoryID = created.ID
	version.MemoryExternalID = created.ExternalID
	insertedVersion, err := insertMemoryVersion(ctx, tx, version)
	if err != nil {
		return Memory{}, err
	}
	updated, err := getMemorySQLX(ctx, tx, `
		update memories
		set current_version_id = :version_id,
			current_version_external_id = :version_external_id
		where workspace_id = :workspace_id and id = :memory_id
		returning `+memoryColumns()+`
	`, map[string]any{
		"workspace_id":        store.WorkspaceID,
		"memory_id":           created.ID,
		"version_id":          insertedVersion.ID,
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

func (d *DB) GetMemory(ctx context.Context, workspaceID int64, memoryStoreExternalID, memoryExternalID string) (Memory, error) {
	return getMemorySQLX(ctx, d.sql, memorySelectSQL()+`
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and external_id = :memory_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_id":             workspaceID,
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

	current, storeArchived, err := d.getActiveMemoryForMutation(ctx, tx, input.WorkspaceID, input.MemoryStoreExternalID, input.MemoryExternalID)
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
		if err := d.ensureMemoryPathAvailable(ctx, tx, current.MemoryStoreID, targetPath, current.ID); err != nil {
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
		OrganizationID:        current.OrganizationID,
		WorkspaceID:           current.WorkspaceID,
		MemoryStoreID:         current.MemoryStoreID,
		MemoryStoreExternalID: current.MemoryStoreExternalID,
		MemoryID:              current.ID,
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
		set current_version_id = :version_id,
			current_version_external_id = :version_external_id,
			path = :path,
			content_size_bytes = :content_size_bytes,
			content_sha256 = :content_sha256,
			s3_bucket = :s3_bucket,
			s3_key = :s3_key,
			updated_at = :updated_at
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and external_id = :memory_external_id
			and deleted_at is null
		returning `+memoryColumns()+`
	`, map[string]any{
		"workspace_id":             input.WorkspaceID,
		"memory_store_external_id": input.MemoryStoreExternalID,
		"memory_external_id":       input.MemoryExternalID,
		"version_id":               version.ID,
		"version_external_id":      version.ExternalID,
		"path":                     targetPath,
		"content_size_bytes":       targetSize,
		"content_sha256":           targetSHA,
		"s3_bucket":                targetBucket,
		"s3_key":                   targetKey,
		"updated_at":               input.Now,
	})
	if isUniqueViolation(err) {
		return MemoryMutationResult{}, d.memoryPathConflict(ctx, tx, current.MemoryStoreID, targetPath)
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

	current, storeArchived, err := d.getActiveMemoryForMutation(ctx, tx, input.WorkspaceID, input.MemoryStoreExternalID, input.MemoryExternalID)
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
		OrganizationID:        current.OrganizationID,
		WorkspaceID:           current.WorkspaceID,
		MemoryStoreID:         current.MemoryStoreID,
		MemoryStoreExternalID: current.MemoryStoreExternalID,
		MemoryID:              current.ID,
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
		set current_version_id = :version_id,
			current_version_external_id = :version_external_id,
			updated_at = :updated_at,
			deleted_at = :updated_at
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and external_id = :memory_external_id
			and deleted_at is null
	`, map[string]any{
		"workspace_id":             input.WorkspaceID,
		"memory_store_external_id": input.MemoryStoreExternalID,
		"memory_external_id":       input.MemoryExternalID,
		"version_id":               version.ID,
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
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceID, params.MemoryStoreExternalID); err != nil {
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
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_id":             params.WorkspaceID,
		"memory_store_external_id": params.MemoryStoreExternalID,
		"limit":                    params.Limit + 1,
	}
	if params.PathPrefix != "" {
		query += " and left(path, length(:path_prefix)) = :path_prefix"
		arguments["path_prefix"] = params.PathPrefix
	}
	if params.Cursor != nil {
		arguments["cursor_id"] = params.Cursor.ID
		switch orderBy {
		case "path":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (path %s :cursor_path or (path = :cursor_path and id %s :cursor_id))", op, op)
			arguments["cursor_path"] = params.Cursor.Path
		case "created_at":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (created_at %s :cursor_created_at or (created_at = :cursor_created_at and id %s :cursor_id))", op, op)
			arguments["cursor_created_at"] = params.Cursor.CreatedAt
		case "updated_at":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (updated_at %s :cursor_updated_at or (updated_at = :cursor_updated_at and id %s :cursor_id))", op, op)
			arguments["cursor_updated_at"] = params.Cursor.UpdatedAt
		}
	}
	query += fmt.Sprintf(" order by %s %s, id %s limit :limit", orderBy, order, order)

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
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceID, params.MemoryStoreExternalID); err != nil {
		return nil, err
	}
	query := memorySelectSQL() + `
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and deleted_at is null
	`
	arguments := map[string]any{
		"workspace_id":             params.WorkspaceID,
		"memory_store_external_id": params.MemoryStoreExternalID,
	}
	if params.PathPrefix != "" {
		query += " and left(path, length(:path_prefix)) = :path_prefix"
		arguments["path_prefix"] = params.PathPrefix
	}
	query += " order by path asc, id asc"
	return selectMemoriesSQLX(ctx, d.sql, query, arguments)
}

func (d *DB) GetMemoryVersion(ctx context.Context, workspaceID int64, memoryStoreExternalID, versionExternalID string) (MemoryVersion, error) {
	return getMemoryVersionSQLX(ctx, d.sql, memoryVersionSelectSQL()+`
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and external_id = :version_external_id
	`, map[string]any{
		"workspace_id":             workspaceID,
		"memory_store_external_id": memoryStoreExternalID,
		"version_external_id":      versionExternalID,
	})
}

func (d *DB) ListMemoryVersionsPage(ctx context.Context, params ListMemoryVersionsPageParams) ([]MemoryVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceID, params.MemoryStoreExternalID); err != nil {
		return nil, false, err
	}
	query := memoryVersionSelectSQL() + `
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
	`
	arguments := map[string]any{
		"workspace_id":             params.WorkspaceID,
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
		query += " and (created_at < :cursor_created_at or (created_at = :cursor_created_at and id < :cursor_id))"
		arguments["cursor_created_at"] = params.Cursor.CreatedAt
		arguments["cursor_id"] = params.Cursor.ID
	}
	query += " order by created_at desc, id desc limit :limit"

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

func (d *DB) RedactMemoryVersion(ctx context.Context, workspaceID int64, memoryStoreExternalID, versionExternalID string, actor MemoryActor, now time.Time) (MemoryVersion, *ObjectRef, error) {
	tx, err := d.sql.BeginTxx(ctx, nil)
	if err != nil {
		return MemoryVersion{}, nil, err
	}
	defer tx.Rollback()

	arguments := map[string]any{
		"workspace_id":             workspaceID,
		"memory_store_external_id": memoryStoreExternalID,
		"version_external_id":      versionExternalID,
	}
	version, err := getMemoryVersionSQLX(ctx, tx, memoryVersionSelectSQL()+`
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and external_id = :version_external_id
		for update
	`, arguments)
	if err != nil {
		return MemoryVersion{}, nil, err
	}

	var activeHead int
	arguments["version_id"] = version.ID
	if err := namedGetContext(ctx, tx, &activeHead, `
		select CAST(count(*) AS integer)
		from memories
		where workspace_id = :workspace_id
			and memory_store_external_id = :memory_store_external_id
			and current_version_id = :version_id
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
			WorkspaceID:  version.WorkspaceID,
			ResourceType: "memory_version",
			ResourceID:   version.ExternalID,
		}
		if version.S3Bucket != nil {
			ref.Bucket = *version.S3Bucket
		}
		ref.Key = *version.S3Key
	}
	arguments["now"] = now
	arguments["actor_type"] = actor.Type
	arguments["api_key_id"] = nullableInt64(actor.APIKeyID)
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
			redacted_by_api_key_id = :api_key_id,
			redacted_by_api_key_external_id = :api_key_external_id,
			redacted_by_session_id = :session_id,
			redacted_by_user_id = :user_id
		where workspace_id = :workspace_id
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

func (d *DB) getActiveMemoryForMutation(ctx context.Context, tx sqlxNamedQueryer, workspaceID int64, memoryStoreExternalID, memoryExternalID string) (Memory, bool, error) {
	var row activeMemoryRow
	err := namedGetContext(ctx, tx, &row, `
		select m.id, CAST(m.uuid AS text) AS uuid, m.external_id, m.organization_id, m.workspace_id,
			m.memory_store_id, m.memory_store_external_id,
			coalesce(m.current_version_id, 0) as current_version_id,
			coalesce(m.current_version_external_id, '') as current_version_external_id,
			m.path, m.content_size_bytes,
			m.content_sha256, m.s3_bucket, m.s3_key, m.created_at, m.updated_at,
			m.deleted_at, ms.archived_at AS archived_at
		from memories m
		join memory_stores ms on ms.id = m.memory_store_id
		where m.workspace_id = :workspace_id
			and m.memory_store_external_id = :memory_store_external_id
			and m.external_id = :memory_external_id
			and m.deleted_at is null
			and ms.deleted_at is null
		for update of m, ms
	`, map[string]any{
		"workspace_id":             workspaceID,
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

func (d *DB) ensureMemoryStoreExists(ctx context.Context, workspaceID int64, memoryStoreExternalID string) error {
	var id int64
	if err := namedGetContext(ctx, d.sql, &id, `
		select id
		from memory_stores
		where workspace_id = :workspace_id and external_id = :external_id and deleted_at is null
	`, map[string]any{"workspace_id": workspaceID, "external_id": memoryStoreExternalID}); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

func (d *DB) ensureMemoryPathAvailable(ctx context.Context, tx sqlxNamedQueryer, storeID int64, path string, excludeMemoryID int64) error {
	var existingID string
	if err := namedGetContext(ctx, tx, &existingID, `
		select external_id
		from memories
		where memory_store_id = :store_id
			and path = :path
			and id <> :exclude_memory_id
			and deleted_at is null
	`, map[string]any{"store_id": storeID, "path": path, "exclude_memory_id": excludeMemoryID}); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
}

func (d *DB) memoryPathConflict(ctx context.Context, database sqlxNamedQueryer, storeID int64, path string) error {
	var existingID string
	if err := namedGetContext(ctx, database, &existingID, `
		select external_id
		from memories
		where memory_store_id = :store_id
			and path = :path
			and deleted_at is null
		limit 1
	`, map[string]any{"store_id": storeID, "path": path}); err == nil {
		return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
	}
	return ErrDuplicate
}

const insertMemoryVersionQuery = `
		insert into memory_versions (
			uuid, external_id, organization_id, workspace_id, memory_store_id,
			memory_store_external_id, memory_id, memory_external_id, operation, path,
			content_size_bytes, content_sha256, s3_bucket, s3_key, created_by_actor_type,
			created_by_api_key_id, created_by_api_key_external_id, created_by_session_id,
			created_by_user_id, created_at
		)
		values (
			:uuid, :external_id, :organization_id, :workspace_id, :memory_store_id,
			:memory_store_external_id, :memory_id, :memory_external_id, :operation, :path,
			:content_size_bytes, :content_sha256, :s3_bucket, :s3_key, :created_by_actor_type,
			:created_by_api_key_id, :created_by_api_key_external_id, :created_by_session_id,
			:created_by_user_id, :created_at
		)
		returning ` + `id, CAST(uuid AS text) AS uuid, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, memory_id, memory_external_id,
			operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
			created_by_actor_type, created_by_api_key_id, created_by_api_key_external_id,
			created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
			redacted_by_api_key_id, redacted_by_api_key_external_id, redacted_by_session_id,
			redacted_by_user_id, created_at`

func insertMemoryVersion(ctx context.Context, tx sqlxNamedQueryer, version MemoryVersion) (MemoryVersion, error) {
	return getMemoryVersionSQLX(ctx, tx, insertMemoryVersionQuery, map[string]any{
		"uuid":                           version.UUID,
		"external_id":                    version.ExternalID,
		"organization_id":                version.OrganizationID,
		"workspace_id":                   version.WorkspaceID,
		"memory_store_id":                version.MemoryStoreID,
		"memory_store_external_id":       version.MemoryStoreExternalID,
		"memory_id":                      version.MemoryID,
		"memory_external_id":             version.MemoryExternalID,
		"operation":                      version.Operation,
		"path":                           nullableStringPtr(version.Path),
		"content_size_bytes":             nullableInt64Ptr(version.ContentSizeBytes),
		"content_sha256":                 nullableStringPtr(version.ContentSHA256),
		"s3_bucket":                      nullableStringPtr(version.S3Bucket),
		"s3_key":                         nullableStringPtr(version.S3Key),
		"created_by_actor_type":          version.CreatedBy.Type,
		"created_by_api_key_id":          nullableInt64(version.CreatedBy.APIKeyID),
		"created_by_api_key_external_id": nullableString(version.CreatedBy.APIKeyExternalID),
		"created_by_session_id":          nullableString(version.CreatedBy.SessionID),
		"created_by_user_id":             nullableString(version.CreatedBy.UserID),
		"created_at":                     version.CreatedAt,
	})
}

func memoryStoreColumns() string {
	return `id, CAST(uuid AS text) as uuid, external_id, organization_id, workspace_id,
		created_by_api_key_id, name, description, metadata, created_at, updated_at,
		archived_at, deleted_at`
}

func memoryStoreSelectSQL() string {
	return `select ` + memoryStoreColumns() + ` from memory_stores`
}

func memoryColumns() string {
	return `id, CAST(uuid AS text) as uuid, external_id, organization_id, workspace_id,
		memory_store_id, memory_store_external_id,
		coalesce(current_version_id, 0) as current_version_id,
		coalesce(current_version_external_id, '') as current_version_external_id,
		path, content_size_bytes, content_sha256, s3_bucket, s3_key,
		created_at, updated_at, deleted_at`
}

func memorySelectSQL() string {
	return `select ` + memoryColumns() + ` from memories`
}

func memoryVersionColumns() string {
	return `id, CAST(uuid AS text) as uuid, external_id, organization_id, workspace_id,
		memory_store_id, memory_store_external_id, memory_id, memory_external_id,
		operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
		created_by_actor_type, created_by_api_key_id, created_by_api_key_external_id,
		created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
		redacted_by_api_key_id, redacted_by_api_key_external_id, redacted_by_session_id,
		redacted_by_user_id, created_at`
}

func memoryVersionSelectSQL() string {
	return `select ` + memoryVersionColumns() + ` from memory_versions`
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

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
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

func nullInt64Value(value sql.NullInt64) int64 {
	if !value.Valid {
		return 0
	}
	return value.Int64
}
