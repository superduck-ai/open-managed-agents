package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type memoryStoreRow struct {
	UUID                string     `db:"uuid"`
	ExternalID          string     `db:"external_id"`
	OrganizationUUID    string     `db:"organization_uuid"`
	WorkspaceUUID       string     `db:"workspace_uuid"`
	CreatedByAPIKeyUUID string     `db:"created_by_api_key_uuid"`
	Name                string     `db:"name"`
	Description         string     `db:"description"`
	Metadata            []byte     `db:"metadata"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
	ArchivedAt          *time.Time `db:"archived_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
}

type memoryRow struct {
	UUID                     string     `db:"uuid"`
	ExternalID               string     `db:"external_id"`
	OrganizationUUID         string     `db:"organization_uuid"`
	WorkspaceUUID            string     `db:"workspace_uuid"`
	MemoryStoreUUID          string     `db:"memory_store_uuid"`
	MemoryStoreExternalID    string     `db:"memory_store_external_id"`
	CurrentVersionUUID       string     `db:"current_version_uuid"`
	CurrentVersionExternalID string     `db:"current_version_external_id"`
	Path                     string     `db:"path"`
	ContentSizeBytes         int64      `db:"content_size_bytes"`
	ContentSHA256            string     `db:"content_sha256"`
	S3Bucket                 string     `db:"s3_bucket"`
	S3Key                    string     `db:"s3_key"`
	CreatedAt                time.Time  `db:"created_at"`
	UpdatedAt                time.Time  `db:"updated_at"`
	DeletedAt                *time.Time `db:"deleted_at"`
}

type activeMemoryRow struct {
	memoryRow
	ArchivedAt *time.Time `db:"archived_at"`
}

type memoryVersionRow struct {
	UUID                       string         `db:"uuid"`
	ExternalID                 string         `db:"external_id"`
	OrganizationUUID           string         `db:"organization_uuid"`
	WorkspaceUUID              string         `db:"workspace_uuid"`
	MemoryStoreUUID            string         `db:"memory_store_uuid"`
	MemoryStoreExternalID      string         `db:"memory_store_external_id"`
	MemoryUUID                 string         `db:"memory_uuid"`
	MemoryExternalID           string         `db:"memory_external_id"`
	Operation                  string         `db:"operation"`
	Path                       sql.NullString `db:"path"`
	ContentSizeBytes           sql.NullInt64  `db:"content_size_bytes"`
	ContentSHA256              sql.NullString `db:"content_sha256"`
	S3Bucket                   sql.NullString `db:"s3_bucket"`
	S3Key                      sql.NullString `db:"s3_key"`
	CreatedByActorType         string         `db:"created_by_actor_type"`
	CreatedByAPIKeyUUID        sql.NullString `db:"created_by_api_key_uuid"`
	CreatedByAPIKeyExternalID  sql.NullString `db:"created_by_api_key_external_id"`
	CreatedBySessionID         sql.NullString `db:"created_by_session_id"`
	CreatedByUserID            sql.NullString `db:"created_by_user_id"`
	RedactedAt                 *time.Time     `db:"redacted_at"`
	RedactedByActorType        sql.NullString `db:"redacted_by_actor_type"`
	RedactedByAPIKeyUUID       sql.NullString `db:"redacted_by_api_key_uuid"`
	RedactedByAPIKeyExternalID sql.NullString `db:"redacted_by_api_key_external_id"`
	RedactedBySessionID        sql.NullString `db:"redacted_by_session_id"`
	RedactedByUserID           sql.NullString `db:"redacted_by_user_id"`
	CreatedAt                  time.Time      `db:"created_at"`
}

type objectRefRow struct {
	WorkspaceUUID string `db:"workspace_uuid"`
	Bucket        string `db:"bucket"`
	Key           string `db:"key"`
	ResourceID    string `db:"resource_id"`
}

func getMemoryStoreSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (MemoryStore, error) {
	var row memoryStoreRow
	if err := namedGetContext(ctx, database, &row, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return MemoryStore{}, ErrNotFound
	} else if err != nil {
		return MemoryStore{}, err
	}
	return row.store(), nil
}

func selectMemoryStoresSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) ([]MemoryStore, error) {
	var rows []memoryStoreRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	stores := make([]MemoryStore, len(rows))
	for index := range rows {
		stores[index] = rows[index].store()
	}
	return stores, nil
}

func getMemorySQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (Memory, error) {
	var row memoryRow
	if err := namedGetContext(ctx, database, &row, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return Memory{}, ErrNotFound
	} else if err != nil {
		return Memory{}, err
	}
	return row.memory(), nil
}

func selectMemoriesSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) ([]Memory, error) {
	var rows []memoryRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	memories := make([]Memory, len(rows))
	for index := range rows {
		memories[index] = rows[index].memory()
	}
	return memories, nil
}

func getMemoryVersionSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) (MemoryVersion, error) {
	var row memoryVersionRow
	if err := namedGetContext(ctx, database, &row, query, arguments); errors.Is(err, sql.ErrNoRows) {
		return MemoryVersion{}, ErrNotFound
	} else if err != nil {
		return MemoryVersion{}, err
	}
	return row.version(), nil
}

func selectMemoryVersionsSQLX(ctx context.Context, database sqlxNamedQueryer, query string, arguments map[string]any) ([]MemoryVersion, error) {
	var rows []memoryVersionRow
	if err := namedSelectContext(ctx, database, &rows, query, arguments); err != nil {
		return nil, err
	}
	versions := make([]MemoryVersion, len(rows))
	for index := range rows {
		versions[index] = rows[index].version()
	}
	return versions, nil
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
		Metadata:            copyRaw(r.Metadata),
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
		CurrentVersionUUID:       r.CurrentVersionUUID,
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
		Path:                  nullStringPtr(r.Path),
		ContentSizeBytes:      nullInt64Ptr(r.ContentSizeBytes),
		ContentSHA256:         nullStringPtr(r.ContentSHA256),
		S3Bucket:              nullStringPtr(r.S3Bucket),
		S3Key:                 nullStringPtr(r.S3Key),
		CreatedBy: MemoryActor{
			Type:             r.CreatedByActorType,
			APIKeyUUID:       nullStringValue(r.CreatedByAPIKeyUUID),
			APIKeyExternalID: nullStringValue(r.CreatedByAPIKeyExternalID),
			SessionID:        nullStringValue(r.CreatedBySessionID),
			UserID:           nullStringValue(r.CreatedByUserID),
		},
		RedactedAt: r.RedactedAt,
		CreatedAt:  r.CreatedAt,
	}
	if r.RedactedByActorType.Valid {
		version.RedactedBy = &MemoryActor{
			Type:             r.RedactedByActorType.String,
			APIKeyUUID:       nullStringValue(r.RedactedByAPIKeyUUID),
			APIKeyExternalID: nullStringValue(r.RedactedByAPIKeyExternalID),
			SessionID:        nullStringValue(r.RedactedBySessionID),
			UserID:           nullStringValue(r.RedactedByUserID),
		}
	}
	return version
}
