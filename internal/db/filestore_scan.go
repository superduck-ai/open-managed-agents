package db

import (
	"bytes"
	"encoding/json"
)

func filestoreFilesystemSelectSQL() string {
	return "select " + filestoreFilesystemColumns() + " from filestore_filesystems"
}

func filestoreFilesystemColumns() string {
	return `id,
		coalesce((select s.id from sessions s where s.uuid = filestore_filesystems.session_uuid), 0) as session_id,
		cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(session_uuid as text) as session_uuid,
		cast(code_session_uuid as text) as code_session_uuid,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		created_at, updated_at, deleted_at`
}

func sessionNamespaceNodeSelectSQL() string {
	return `select ` + sessionNamespaceNodeColumns() + ` from (` + sessionNamespaceNodeSourceSQL() + `) namespace_nodes`
}

// sessionNamespaceNodeSourceSQL 将统一后的 Resource + File 模型映射为 Filestore
// 服务当前使用的节点 DTO。它不是数据库兼容视图，也不会形成第二套读模型。
func sessionNamespaceNodeSourceSQL() string {
	return `
		select resource.id,
			cast(resource.uuid as text) as uuid,
			resource.external_id,
			cast(organization.uuid as text) as organization_uuid,
			cast(workspace.uuid as text) as workspace_uuid,
			cast(filesystem.uuid as text) as filesystem_uuid,
			case resource.resource_type
				when 'skill_archive' then 'archive'
				else resource.resource_type
			end as kind,
			resource.path,
			resource.parent_path,
			case when resource.resource_type = 'skill_archive'
				then coalesce(custom_version.size_bytes, builtin_version.size_bytes)
				else file.size_bytes
			end as size_bytes,
			case when resource.resource_type = 'skill_archive'
				then 'application/zip'
				else file.mime_type
			end as media_type,
			case when resource.resource_type = 'skill_archive'
				then 'application/zip'
				else file.detected_mime_type
			end as detected_mime_type,
			case when resource.resource_type = 'skill_archive'
				then jsonb_build_object(
					'skill_source', case when custom_version.id is not null then 'custom' else 'anthropic' end
				)
				else coalesce(file.metadata, cast('{}' as jsonb))
			end as metadata,
			coalesce(file.authorization_metadata, cast('{}' as jsonb)) as authorization_metadata,
			cast(coalesce(to_jsonb(file.tags), cast('[]' as jsonb)) as text) as tags_json,
			coalesce(file.downloadable, false) as downloadable,
			file.md5,
			case when resource.resource_type = 'skill_archive'
				then coalesce(custom_version.sha256, builtin_version.sha256)
				else file.sha256
			end as sha256,
			case when resource.resource_type = 'skill_archive'
				then coalesce(custom_version.s3_bucket, builtin_version.s3_bucket)
				else file.s3_bucket
			end as s3_bucket,
			case when resource.resource_type = 'skill_archive'
				then coalesce(custom_version.s3_key, builtin_version.s3_key)
				else file.s3_key
			end as s3_key,
			file.s3_etag,
			file.s3_version_id,
			resource.expires_at,
			cast(resource.skill_version_uuid as text) as skill_version_uuid,
			case when resource.payload is not null
				then cast(resource.file_uuid as text)
				else null
			end as referenced_file_uuid,
			cast(api_key.uuid as text) as created_by_api_key_uuid,
			cast(session.uuid as text) as created_by_session_uuid,
			cast(filesystem.code_session_uuid as text) as created_by_code_session_uuid,
			resource.created_at,
			resource.updated_at,
			resource.deleted_at
		from session_resources resource
		join sessions session
			on session.id = resource.session_id
			and session.workspace_id = resource.workspace_id
		join workspaces workspace on workspace.id = resource.workspace_id
		join organizations organization on organization.id = resource.organization_id
		join filestore_filesystems filesystem
			on filesystem.session_uuid = session.uuid
			and filesystem.workspace_uuid = workspace.uuid
		left join files file
			on file.uuid = resource.file_uuid
			and file.workspace_id = resource.workspace_id
		left join api_keys api_key on api_key.id = file.created_by_api_key_id
		left join skill_versions custom_version
			on custom_version.uuid = resource.skill_version_uuid
			and custom_version.workspace_id = resource.workspace_id
		left join builtin_skill_versions builtin_version
			on builtin_version.uuid = resource.skill_version_uuid
		where resource.path is not null
	`
}

func sessionNamespaceNodeColumns() string {
	return `id, cast(uuid as text) as uuid, external_id,
		cast(organization_uuid as text) as organization_uuid,
		cast(workspace_uuid as text) as workspace_uuid,
		cast(filesystem_uuid as text) as filesystem_uuid, kind, path, parent_path,
		size_bytes, media_type, detected_mime_type, metadata, authorization_metadata,
		tags_json,
		downloadable, md5, sha256, s3_bucket, s3_key, s3_etag, s3_version_id,
		expires_at, cast(skill_version_uuid as text) as skill_version_uuid,
		cast(referenced_file_uuid as text) as referenced_file_uuid,
		cast(created_by_api_key_uuid as text) as created_by_api_key_uuid,
		cast(created_by_session_uuid as text) as created_by_session_uuid,
		cast(created_by_code_session_uuid as text) as created_by_code_session_uuid,
		created_at, updated_at, deleted_at`
}

func virtualFilestoreRoot(filesystem FilestoreFilesystem) SessionNamespaceNode {
	// 根目录与文件系统同生共灭，虚拟投影可省去一条永远存在且不可删除的特殊 entries 记录。
	return SessionNamespaceNode{
		UUID:                  filesystem.UUID,
		ExternalID:            filesystem.ExternalID,
		OrganizationUUID:      filesystem.OrganizationUUID,
		WorkspaceUUID:         filesystem.WorkspaceUUID,
		FilesystemUUID:        filesystem.UUID,
		Kind:                  SessionNamespaceNodeKindDirectory,
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
