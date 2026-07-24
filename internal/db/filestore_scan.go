package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// 下列 PGX 适配器只服务于必须加入既有 pgx.Tx 的事务链。
// 非事务查询统一使用 filestore_sqlx.go 中的命名查询和结构体映射。
type filestorePGXQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type filestorePGXRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func filestoreFilesystemSelectSQL() string {
	return "select " + filestoreFilesystemColumns() + " from filestore_filesystems"
}

func filestoreFilesystemColumns() string {
	return `id, cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(session_uuid as text) as session_uuid,
		cast(code_session_uuid as text) as code_session_uuid,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		created_at, updated_at, deleted_at`
}

func scanFilestoreFilesystemPGX(row filestorePGXScanner) (FilestoreFilesystem, error) {
	var databaseRow filestoreFilesystemRow
	err := row.Scan(&databaseRow.ID, &databaseRow.UUID, &databaseRow.ExternalID,
		&databaseRow.OrganizationUUID, &databaseRow.WorkspaceUUID, &databaseRow.SessionUUID,
		&databaseRow.CodeSessionUUID, &databaseRow.CreatedByAPIKeyUUID,
		&databaseRow.CreatedAt, &databaseRow.UpdatedAt, &databaseRow.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FilestoreFilesystem{}, ErrNotFound
	}
	if err != nil {
		return FilestoreFilesystem{}, err
	}
	return databaseRow.filesystem(), nil
}

func filestoreEntrySelectSQL() string {
	return "select " + filestoreEntryColumns() + " from filestore_entries"
}

func filestoreEntryColumns() string {
	return `id, cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(filesystem_uuid as text) as filesystem_uuid, kind, path, parent_path,
		size_bytes, media_type, detected_mime_type, metadata, authorization_metadata,
		cast(coalesce(to_jsonb(tags), cast('[]' as jsonb)) as text) as tags_json,
		downloadable, md5, sha256, s3_bucket, s3_key, s3_etag, s3_version_id,
		expires_at, managed_by, cast(managed_resource_uuid as text) as managed_resource_uuid,
		cast(source_file_uuid as text) as source_file_uuid,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		cast(created_by_session_uuid as text) as created_by_session_uuid,
		cast(created_by_code_session_uuid as text) as created_by_code_session_uuid,
		created_at, updated_at, deleted_at`
}

type filestorePGXScanner interface {
	Scan(...any) error
}

func scanFilestoreEntryPGX(row filestorePGXScanner) (FilestoreEntry, error) {
	var databaseRow filestoreEntryRow
	err := row.Scan(&databaseRow.ID, &databaseRow.UUID, &databaseRow.ExternalID,
		&databaseRow.OrganizationUUID, &databaseRow.WorkspaceUUID, &databaseRow.FilesystemUUID,
		&databaseRow.Kind, &databaseRow.Path, &databaseRow.ParentPath, &databaseRow.SizeBytes,
		&databaseRow.MediaType, &databaseRow.DetectedMimeType, &databaseRow.Metadata,
		&databaseRow.AuthorizationMetadata, &databaseRow.TagsJSON, &databaseRow.Downloadable,
		&databaseRow.MD5, &databaseRow.SHA256, &databaseRow.S3Bucket, &databaseRow.S3Key,
		&databaseRow.S3ETag, &databaseRow.S3VersionID, &databaseRow.ExpiresAt,
		&databaseRow.ManagedBy, &databaseRow.ManagedResourceUUID,
		&databaseRow.SourceFileUUID,
		&databaseRow.CreatedByAPIKeyUUID, &databaseRow.CreatedBySessionUUID,
		&databaseRow.CreatedByCodeSessionUUID, &databaseRow.CreatedAt, &databaseRow.UpdatedAt,
		&databaseRow.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return FilestoreEntry{}, ErrNotFound
	}
	if err != nil {
		return FilestoreEntry{}, err
	}
	return databaseRow.entry()
}

func scanFilestoreEntryRowsPGX(rows filestorePGXRows) ([]FilestoreEntry, error) {
	var entries []FilestoreEntry
	for rows.Next() {
		entry, err := scanFilestoreEntryPGX(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func virtualFilestoreRoot(filesystem FilestoreFilesystem) FilestoreEntry {
	// 根目录与文件系统同生共灭，虚拟投影可省去一条永远存在且不可删除的特殊 entries 记录。
	return FilestoreEntry{
		UUID:                  filesystem.UUID,
		ExternalID:            filesystem.ExternalID,
		OrganizationUUID:      filesystem.OrganizationUUID,
		WorkspaceUUID:         filesystem.WorkspaceUUID,
		FilesystemUUID:        filesystem.UUID,
		Kind:                  FilestoreEntryKindDirectory,
		Path:                  "/",
		Metadata:              json.RawMessage(`{}`),
		AuthorizationMetadata: json.RawMessage(`{}`),
		Tags:                  []string{},
		CreatedAt:             filesystem.CreatedAt,
		UpdatedAt:             filesystem.UpdatedAt,
	}
}

func filestoreJSONObject(value json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte(`{}`)
	}
	return trimmed
}

func filestoreTags(value []string) []string {
	if value == nil {
		return []string{}
	}
	return value
}

func filestoreNullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func filestoreString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func filestoreInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
