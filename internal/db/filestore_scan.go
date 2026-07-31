package db

import (
	"bytes"
	"encoding/json"
)

func filestoreFilesystemSelectSQL() string {
	return "select " + filestoreFilesystemColumns() + " from filestore_filesystems"
}

func filestoreFilesystemColumns() string {
	return `cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(session_uuid as text) as session_uuid,
		cast(code_session_uuid as text) as code_session_uuid,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		created_at, updated_at, deleted_at`
}

func filestoreEntrySelectSQL() string {
	return "select " + filestoreEntryColumns() + " from filestore_entries"
}

func filestoreEntryColumns() string {
	return `cast(uuid as text) as uuid, external_id,
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
