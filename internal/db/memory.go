package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
	return scanMemoryStore(d.Pool.QueryRow(ctx, `
		insert into memory_stores (
			uuid, external_id, organization_id, workspace_id, created_by_api_key_id,
			name, description, metadata, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $9)
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, name, description, metadata, created_at, updated_at,
			archived_at, deleted_at
	`, store.UUID, store.ExternalID, store.OrganizationID, store.WorkspaceID, store.CreatedByAPIKeyID,
		store.Name, store.Description, jsonArg(store.Metadata), store.CreatedAt))
}

func (d *DB) GetMemoryStore(ctx context.Context, workspaceID int64, externalID string) (MemoryStore, error) {
	return scanMemoryStore(d.Pool.QueryRow(ctx, memoryStoreSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, externalID))
}

func (d *DB) GetMemoryStoreByExternalID(ctx context.Context, externalID string) (MemoryStore, error) {
	return scanMemoryStore(d.Pool.QueryRow(ctx, memoryStoreSelectSQL()+`
		where external_id = $1 and deleted_at is null
	`, externalID))
}

func (d *DB) UpdateMemoryStore(ctx context.Context, workspaceID int64, externalID string, next MemoryStore) (MemoryStore, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return MemoryStore{}, err
	}
	defer tx.Rollback(ctx)

	current, err := scanMemoryStore(tx.QueryRow(ctx, memoryStoreSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID))
	if err != nil {
		return MemoryStore{}, err
	}
	if current.ArchivedAt != nil {
		return MemoryStore{}, ErrInvalidState
	}
	updated, err := scanMemoryStore(tx.QueryRow(ctx, `
		update memory_stores
		set name = $3,
			description = $4,
			metadata = $5::jsonb,
			updated_at = $6
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, name, description, metadata, created_at, updated_at,
			archived_at, deleted_at
	`, workspaceID, externalID, next.Name, next.Description, jsonArg(next.Metadata), next.UpdatedAt))
	if err != nil {
		return MemoryStore{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemoryStore{}, err
	}
	return updated, nil
}

func (d *DB) ArchiveMemoryStore(ctx context.Context, workspaceID int64, externalID string) (MemoryStore, error) {
	return scanMemoryStore(d.Pool.QueryRow(ctx, `
		update memory_stores
		set archived_at = coalesce(archived_at, now()),
			updated_at = now()
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, name, description, metadata, created_at, updated_at,
			archived_at, deleted_at
	`, workspaceID, externalID))
}

func (d *DB) DeleteMemoryStore(ctx context.Context, workspaceID int64, externalID string) ([]ObjectRef, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var storeID int64
	if err := tx.QueryRow(ctx, `
		select id
		from memory_stores
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, workspaceID, externalID).Scan(&storeID); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, `
		select workspace_id, coalesce(s3_bucket, ''), coalesce(s3_key, ''), external_id
		from memory_versions
		where workspace_id = $1
			and memory_store_id = $2
			and s3_key is not null
	`, workspaceID, storeID)
	if err != nil {
		return nil, err
	}
	var refs []ObjectRef
	for rows.Next() {
		var ref ObjectRef
		if err := rows.Scan(&ref.WorkspaceID, &ref.Bucket, &ref.Key, &ref.ResourceID); err != nil {
			rows.Close()
			return nil, err
		}
		ref.ResourceType = "memory_version"
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	if _, err := tx.Exec(ctx, `
		delete from memory_versions
		where workspace_id = $1 and memory_store_id = $2
	`, workspaceID, storeID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		delete from memories
		where workspace_id = $1 and memory_store_id = $2
	`, workspaceID, storeID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		delete from memory_stores
		where workspace_id = $1 and id = $2
	`, workspaceID, storeID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return refs, nil
}

func (d *DB) ListMemoryStoresPage(ctx context.Context, params ListMemoryStoresPageParams) ([]MemoryStore, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	query := memoryStoreSelectSQL() + `
		where workspace_id = $1 and deleted_at is null
	`
	args := []any{params.WorkspaceID}
	nextArg := 2
	if !params.IncludeArchived {
		query += " and archived_at is null"
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	stores, err := scanMemoryStoreRows(rows)
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
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return Memory{}, err
	}
	defer tx.Rollback(ctx)

	store, err := scanMemoryStore(tx.QueryRow(ctx, memoryStoreSelectSQL()+`
		where workspace_id = $1 and external_id = $2 and deleted_at is null
		for update
	`, memory.WorkspaceID, memory.MemoryStoreExternalID))
	if err != nil {
		return Memory{}, err
	}
	if store.ArchivedAt != nil {
		return Memory{}, ErrInvalidState
	}
	if err := d.ensureMemoryPathAvailable(ctx, tx, store.ID, memory.Path, 0); err != nil {
		return Memory{}, err
	}

	created, err := scanMemory(tx.QueryRow(ctx, `
		insert into memories (
			uuid, external_id, organization_id, workspace_id, memory_store_id,
			memory_store_external_id, current_version_external_id, path,
			content_size_bytes, content_sha256, s3_bucket, s3_key, created_at, updated_at
		)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
		returning id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, coalesce(current_version_id, 0),
			coalesce(current_version_external_id, ''), path, content_size_bytes,
			content_sha256, s3_bucket, s3_key, created_at, updated_at, deleted_at
	`, memory.UUID, memory.ExternalID, store.OrganizationID, store.WorkspaceID, store.ID,
		store.ExternalID, version.ExternalID, memory.Path, memory.ContentSizeBytes,
		memory.ContentSHA256, memory.S3Bucket, memory.S3Key, memory.CreatedAt))
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
	updated, err := scanMemory(tx.QueryRow(ctx, `
		update memories
		set current_version_id = $3,
			current_version_external_id = $4
		where workspace_id = $1 and id = $2
		returning id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, coalesce(current_version_id, 0),
			coalesce(current_version_external_id, ''), path, content_size_bytes,
			content_sha256, s3_bucket, s3_key, created_at, updated_at, deleted_at
	`, store.WorkspaceID, created.ID, insertedVersion.ID, insertedVersion.ExternalID))
	if err != nil {
		return Memory{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Memory{}, err
	}
	return updated, nil
}

func (d *DB) GetMemory(ctx context.Context, workspaceID int64, memoryStoreExternalID, memoryExternalID string) (Memory, error) {
	return scanMemory(d.Pool.QueryRow(ctx, memorySelectSQL()+`
		where workspace_id = $1
			and memory_store_external_id = $2
			and external_id = $3
			and deleted_at is null
	`, workspaceID, memoryStoreExternalID, memoryExternalID))
}

func (d *DB) UpdateMemory(ctx context.Context, input UpdateMemoryInput) (MemoryMutationResult, error) {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return MemoryMutationResult{}, err
	}
	defer tx.Rollback(ctx)

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
			if err := tx.Commit(ctx); err != nil {
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
		if err := tx.Commit(ctx); err != nil {
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
	updated, err := scanMemory(tx.QueryRow(ctx, `
		update memories
		set current_version_id = $4,
			current_version_external_id = $5,
			path = $6,
			content_size_bytes = $7,
			content_sha256 = $8,
			s3_bucket = $9,
			s3_key = $10,
			updated_at = $11
		where workspace_id = $1
			and memory_store_external_id = $2
			and external_id = $3
			and deleted_at is null
		returning id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, coalesce(current_version_id, 0),
			coalesce(current_version_external_id, ''), path, content_size_bytes,
			content_sha256, s3_bucket, s3_key, created_at, updated_at, deleted_at
	`, input.WorkspaceID, input.MemoryStoreExternalID, input.MemoryExternalID, version.ID,
		version.ExternalID, targetPath, targetSize, targetSHA, targetBucket, targetKey, input.Now))
	if isUniqueViolation(err) {
		return MemoryMutationResult{}, d.memoryPathConflict(ctx, tx, current.MemoryStoreID, targetPath)
	}
	if err != nil {
		return MemoryMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemoryMutationResult{}, err
	}
	return MemoryMutationResult{Memory: updated, VersionCreated: true}, nil
}

func (d *DB) DeleteMemory(ctx context.Context, input DeleteMemoryInput) error {
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

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
	tag, err := tx.Exec(ctx, `
		update memories
		set current_version_id = $4,
			current_version_external_id = $5,
			updated_at = $6,
			deleted_at = $6
		where workspace_id = $1
			and memory_store_external_id = $2
			and external_id = $3
			and deleted_at is null
	`, input.WorkspaceID, input.MemoryStoreExternalID, input.MemoryExternalID,
		version.ID, version.ExternalID, input.Now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
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
		where workspace_id = $1
			and memory_store_external_id = $2
			and deleted_at is null
	`
	args := []any{params.WorkspaceID, params.MemoryStoreExternalID}
	nextArg := 3
	if params.PathPrefix != "" {
		query += fmt.Sprintf(" and left(path, length($%d)) = $%d", nextArg, nextArg)
		args = append(args, params.PathPrefix)
		nextArg++
	}
	if params.Cursor != nil {
		switch orderBy {
		case "path":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (path %s $%d or (path = $%d and id %s $%d))", op, nextArg, nextArg, op, nextArg+1)
			args = append(args, params.Cursor.Path, params.Cursor.ID)
			nextArg += 2
		case "created_at":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (created_at %s $%d or (created_at = $%d and id %s $%d))", op, nextArg, nextArg, op, nextArg+1)
			args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
			nextArg += 2
		case "updated_at":
			op := ">"
			if order == "desc" {
				op = "<"
			}
			query += fmt.Sprintf(" and (updated_at %s $%d or (updated_at = $%d and id %s $%d))", op, nextArg, nextArg, op, nextArg+1)
			args = append(args, params.Cursor.UpdatedAt, params.Cursor.ID)
			nextArg += 2
		}
	}
	query += fmt.Sprintf(" order by %s %s, id %s limit $%d", orderBy, order, order, nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	memories, err := scanMemoryRows(rows)
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
		where workspace_id = $1
			and memory_store_external_id = $2
			and deleted_at is null
	`
	args := []any{params.WorkspaceID, params.MemoryStoreExternalID}
	if params.PathPrefix != "" {
		query += " and left(path, length($3)) = $3"
		args = append(args, params.PathPrefix)
	}
	query += " order by path asc, id asc"
	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryRows(rows)
}

func (d *DB) GetMemoryVersion(ctx context.Context, workspaceID int64, memoryStoreExternalID, versionExternalID string) (MemoryVersion, error) {
	return scanMemoryVersion(d.Pool.QueryRow(ctx, memoryVersionSelectSQL()+`
		where workspace_id = $1
			and memory_store_external_id = $2
			and external_id = $3
	`, workspaceID, memoryStoreExternalID, versionExternalID))
}

func (d *DB) ListMemoryVersionsPage(ctx context.Context, params ListMemoryVersionsPageParams) ([]MemoryVersion, bool, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}
	if err := d.ensureMemoryStoreExists(ctx, params.WorkspaceID, params.MemoryStoreExternalID); err != nil {
		return nil, false, err
	}
	query := memoryVersionSelectSQL() + `
		where workspace_id = $1
			and memory_store_external_id = $2
	`
	args := []any{params.WorkspaceID, params.MemoryStoreExternalID}
	nextArg := 3
	if params.MemoryExternalID != "" {
		query += fmt.Sprintf(" and memory_external_id = $%d", nextArg)
		args = append(args, params.MemoryExternalID)
		nextArg++
	}
	if params.Operation != "" {
		query += fmt.Sprintf(" and operation = $%d", nextArg)
		args = append(args, params.Operation)
		nextArg++
	}
	if params.APIKeyExternalID != "" {
		query += fmt.Sprintf(" and created_by_api_key_external_id = $%d", nextArg)
		args = append(args, params.APIKeyExternalID)
		nextArg++
	}
	if params.SessionID != "" {
		query += fmt.Sprintf(" and created_by_session_id = $%d", nextArg)
		args = append(args, params.SessionID)
		nextArg++
	}
	if params.CreatedAtGTE != nil {
		query += fmt.Sprintf(" and created_at >= $%d", nextArg)
		args = append(args, *params.CreatedAtGTE)
		nextArg++
	}
	if params.CreatedAtLTE != nil {
		query += fmt.Sprintf(" and created_at <= $%d", nextArg)
		args = append(args, *params.CreatedAtLTE)
		nextArg++
	}
	if params.Cursor != nil {
		query += fmt.Sprintf(" and (created_at < $%d or (created_at = $%d and id < $%d))", nextArg, nextArg, nextArg+1)
		args = append(args, params.Cursor.CreatedAt, params.Cursor.ID)
		nextArg += 2
	}
	query += fmt.Sprintf(" order by created_at desc, id desc limit $%d", nextArg)
	args = append(args, params.Limit+1)

	rows, err := d.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	versions, err := scanMemoryVersionRows(rows)
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
	tx, err := d.Pool.Begin(ctx)
	if err != nil {
		return MemoryVersion{}, nil, err
	}
	defer tx.Rollback(ctx)

	version, err := scanMemoryVersion(tx.QueryRow(ctx, memoryVersionSelectSQL()+`
		where workspace_id = $1
			and memory_store_external_id = $2
			and external_id = $3
		for update
	`, workspaceID, memoryStoreExternalID, versionExternalID))
	if err != nil {
		return MemoryVersion{}, nil, err
	}

	var activeHead int
	if err := tx.QueryRow(ctx, `
		select count(*)::int
		from memories
		where workspace_id = $1
			and memory_store_external_id = $2
			and current_version_id = $3
			and deleted_at is null
	`, workspaceID, memoryStoreExternalID, version.ID).Scan(&activeHead); err != nil {
		return MemoryVersion{}, nil, err
	}
	if activeHead > 0 {
		return MemoryVersion{}, nil, ErrInvalidState
	}
	if version.RedactedAt != nil {
		if err := tx.Commit(ctx); err != nil {
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
	updated, err := scanMemoryVersion(tx.QueryRow(ctx, `
		update memory_versions
		set path = null,
			content_size_bytes = null,
			content_sha256 = null,
			s3_bucket = null,
			s3_key = null,
			redacted_at = $4,
			redacted_by_actor_type = $5,
			redacted_by_api_key_id = $6,
			redacted_by_api_key_external_id = $7,
			redacted_by_session_id = $8,
			redacted_by_user_id = $9
		where workspace_id = $1
			and memory_store_external_id = $2
			and external_id = $3
		returning id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, memory_id, memory_external_id,
			operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
			created_by_actor_type, created_by_api_key_id, created_by_api_key_external_id,
			created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
			redacted_by_api_key_id, redacted_by_api_key_external_id, redacted_by_session_id,
			redacted_by_user_id, created_at
	`, workspaceID, memoryStoreExternalID, versionExternalID, now, actor.Type, nullableInt64(actor.APIKeyID),
		nullableString(actor.APIKeyExternalID), nullableString(actor.SessionID), nullableString(actor.UserID)))
	if err != nil {
		return MemoryVersion{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MemoryVersion{}, nil, err
	}
	return updated, ref, nil
}

func (d *DB) getActiveMemoryForMutation(ctx context.Context, tx pgx.Tx, workspaceID int64, memoryStoreExternalID, memoryExternalID string) (Memory, bool, error) {
	row := tx.QueryRow(ctx, `
		select m.id, m.uuid::text, m.external_id, m.organization_id, m.workspace_id,
			m.memory_store_id, m.memory_store_external_id, coalesce(m.current_version_id, 0),
			coalesce(m.current_version_external_id, ''), m.path, m.content_size_bytes,
			m.content_sha256, m.s3_bucket, m.s3_key, m.created_at, m.updated_at,
			m.deleted_at, ms.archived_at
		from memories m
		join memory_stores ms on ms.id = m.memory_store_id
		where m.workspace_id = $1
			and m.memory_store_external_id = $2
			and m.external_id = $3
			and m.deleted_at is null
			and ms.deleted_at is null
		for update of m, ms
	`, workspaceID, memoryStoreExternalID, memoryExternalID)
	var memory Memory
	var archivedAt *time.Time
	err := row.Scan(&memory.ID, &memory.UUID, &memory.ExternalID, &memory.OrganizationID,
		&memory.WorkspaceID, &memory.MemoryStoreID, &memory.MemoryStoreExternalID,
		&memory.CurrentVersionID, &memory.CurrentVersionExternalID, &memory.Path,
		&memory.ContentSizeBytes, &memory.ContentSHA256, &memory.S3Bucket, &memory.S3Key,
		&memory.CreatedAt, &memory.UpdatedAt, &memory.DeletedAt, &archivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, false, ErrNotFound
	}
	if err != nil {
		return Memory{}, false, err
	}
	return memory, archivedAt != nil, nil
}

func (d *DB) ensureMemoryStoreExists(ctx context.Context, workspaceID int64, memoryStoreExternalID string) error {
	var id int64
	if err := d.Pool.QueryRow(ctx, `
		select id
		from memory_stores
		where workspace_id = $1 and external_id = $2 and deleted_at is null
	`, workspaceID, memoryStoreExternalID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	return nil
}

func (d *DB) ensureMemoryPathAvailable(ctx context.Context, tx pgx.Tx, storeID int64, path string, excludeMemoryID int64) error {
	var existingID string
	if err := tx.QueryRow(ctx, `
		select external_id
		from memories
		where memory_store_id = $1
			and path = $2
			and id <> $3
			and deleted_at is null
	`, storeID, path, excludeMemoryID).Scan(&existingID); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
}

func (d *DB) memoryPathConflict(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, storeID int64, path string) error {
	var existingID string
	if err := q.QueryRow(ctx, `
		select external_id
		from memories
		where memory_store_id = $1
			and path = $2
			and deleted_at is null
		limit 1
	`, storeID, path).Scan(&existingID); err == nil {
		return &MemoryPathConflictError{ConflictingMemoryID: existingID, ConflictingPath: path}
	}
	return ErrDuplicate
}

func insertMemoryVersion(ctx context.Context, tx pgx.Tx, version MemoryVersion) (MemoryVersion, error) {
	return scanMemoryVersion(tx.QueryRow(ctx, `
		insert into memory_versions (
			uuid, external_id, organization_id, workspace_id, memory_store_id,
			memory_store_external_id, memory_id, memory_external_id, operation, path,
			content_size_bytes, content_sha256, s3_bucket, s3_key, created_by_actor_type,
			created_by_api_key_id, created_by_api_key_external_id, created_by_session_id,
			created_by_user_id, created_at
		)
		values (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20
		)
		returning id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, memory_id, memory_external_id,
			operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
			created_by_actor_type, created_by_api_key_id, created_by_api_key_external_id,
			created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
			redacted_by_api_key_id, redacted_by_api_key_external_id, redacted_by_session_id,
			redacted_by_user_id, created_at
	`, version.UUID, version.ExternalID, version.OrganizationID, version.WorkspaceID,
		version.MemoryStoreID, version.MemoryStoreExternalID, version.MemoryID,
		version.MemoryExternalID, version.Operation, nullableStringPtr(version.Path),
		nullableInt64Ptr(version.ContentSizeBytes), nullableStringPtr(version.ContentSHA256),
		nullableStringPtr(version.S3Bucket), nullableStringPtr(version.S3Key),
		version.CreatedBy.Type, nullableInt64(version.CreatedBy.APIKeyID),
		nullableString(version.CreatedBy.APIKeyExternalID), nullableString(version.CreatedBy.SessionID),
		nullableString(version.CreatedBy.UserID), version.CreatedAt))
}

func memoryStoreSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id,
			created_by_api_key_id, name, description, metadata, created_at, updated_at,
			archived_at, deleted_at
		from memory_stores
	`
}

func memorySelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, coalesce(current_version_id, 0),
			coalesce(current_version_external_id, ''), path, content_size_bytes,
			content_sha256, s3_bucket, s3_key, created_at, updated_at, deleted_at
		from memories
	`
}

func memoryVersionSelectSQL() string {
	return `
		select id, uuid::text, external_id, organization_id, workspace_id,
			memory_store_id, memory_store_external_id, memory_id, memory_external_id,
			operation, path, content_size_bytes, content_sha256, s3_bucket, s3_key,
			created_by_actor_type, created_by_api_key_id, created_by_api_key_external_id,
			created_by_session_id, created_by_user_id, redacted_at, redacted_by_actor_type,
			redacted_by_api_key_id, redacted_by_api_key_external_id, redacted_by_session_id,
			redacted_by_user_id, created_at
		from memory_versions
	`
}

type memoryScanner interface {
	Scan(dest ...any) error
}

type memoryRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanMemoryStore(row memoryScanner) (MemoryStore, error) {
	var store MemoryStore
	var metadata []byte
	err := row.Scan(&store.ID, &store.UUID, &store.ExternalID, &store.OrganizationID,
		&store.WorkspaceID, &store.CreatedByAPIKeyID, &store.Name, &store.Description,
		&metadata, &store.CreatedAt, &store.UpdatedAt, &store.ArchivedAt, &store.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoryStore{}, ErrNotFound
	}
	if err != nil {
		return MemoryStore{}, err
	}
	store.Metadata = copyRaw(metadata)
	return store, nil
}

func scanMemoryStoreRows(rows memoryRows) ([]MemoryStore, error) {
	var stores []MemoryStore
	for rows.Next() {
		store, err := scanMemoryStore(rows)
		if err != nil {
			return nil, err
		}
		stores = append(stores, store)
	}
	return stores, rows.Err()
}

func scanMemory(row memoryScanner) (Memory, error) {
	var memory Memory
	err := row.Scan(&memory.ID, &memory.UUID, &memory.ExternalID, &memory.OrganizationID,
		&memory.WorkspaceID, &memory.MemoryStoreID, &memory.MemoryStoreExternalID,
		&memory.CurrentVersionID, &memory.CurrentVersionExternalID, &memory.Path,
		&memory.ContentSizeBytes, &memory.ContentSHA256, &memory.S3Bucket, &memory.S3Key,
		&memory.CreatedAt, &memory.UpdatedAt, &memory.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, ErrNotFound
	}
	if err != nil {
		return Memory{}, err
	}
	return memory, nil
}

func scanMemoryRows(rows memoryRows) ([]Memory, error) {
	var memories []Memory
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		memories = append(memories, memory)
	}
	return memories, rows.Err()
}

func scanMemoryVersion(row memoryScanner) (MemoryVersion, error) {
	var version MemoryVersion
	var path, contentSHA, bucket, key sql.NullString
	var contentSize sql.NullInt64
	var createdByAPIKeyID, redactedByAPIKeyID sql.NullInt64
	var createdByAPIKeyExternalID, createdBySessionID, createdByUserID sql.NullString
	var redactedByActorType, redactedByAPIKeyExternalID, redactedBySessionID, redactedByUserID sql.NullString
	err := row.Scan(&version.ID, &version.UUID, &version.ExternalID, &version.OrganizationID,
		&version.WorkspaceID, &version.MemoryStoreID, &version.MemoryStoreExternalID,
		&version.MemoryID, &version.MemoryExternalID, &version.Operation, &path,
		&contentSize, &contentSHA, &bucket, &key, &version.CreatedBy.Type,
		&createdByAPIKeyID, &createdByAPIKeyExternalID, &createdBySessionID,
		&createdByUserID, &version.RedactedAt, &redactedByActorType,
		&redactedByAPIKeyID, &redactedByAPIKeyExternalID, &redactedBySessionID,
		&redactedByUserID, &version.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoryVersion{}, ErrNotFound
	}
	if err != nil {
		return MemoryVersion{}, err
	}
	version.Path = nullStringPtr(path)
	version.ContentSizeBytes = nullInt64Ptr(contentSize)
	version.ContentSHA256 = nullStringPtr(contentSHA)
	version.S3Bucket = nullStringPtr(bucket)
	version.S3Key = nullStringPtr(key)
	version.CreatedBy.APIKeyID = nullInt64Value(createdByAPIKeyID)
	version.CreatedBy.APIKeyExternalID = nullStringValue(createdByAPIKeyExternalID)
	version.CreatedBy.SessionID = nullStringValue(createdBySessionID)
	version.CreatedBy.UserID = nullStringValue(createdByUserID)
	if redactedByActorType.Valid {
		actor := MemoryActor{
			Type:             redactedByActorType.String,
			APIKeyID:         nullInt64Value(redactedByAPIKeyID),
			APIKeyExternalID: nullStringValue(redactedByAPIKeyExternalID),
			SessionID:        nullStringValue(redactedBySessionID),
			UserID:           nullStringValue(redactedByUserID),
		}
		version.RedactedBy = &actor
	}
	return version, nil
}

func scanMemoryVersionRows(rows memoryRows) ([]MemoryVersion, error) {
	var versions []MemoryVersion
	for rows.Next() {
		version, err := scanMemoryVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, rows.Err()
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
