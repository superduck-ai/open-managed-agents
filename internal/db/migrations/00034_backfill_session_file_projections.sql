-- +goose Up

-- scoped Files 投影上线前创建的 Session File resource 已经存在活动借用
-- entry。投影不拥有对象字节，因此这里只补齐投影，不修改存储账本。
insert into files (
	uuid,
	external_id,
	workspace_id,
	filename,
	mime_type,
	size_bytes,
	sha256,
	s3_bucket,
	s3_key,
	downloadable,
	scope_type,
	scope_id,
	created_by_api_key_id,
	created_at
)
select
	entry.uuid,
	concat('file_', replace(cast(gen_random_uuid() as text), '-', '')),
	session.workspace_id,
	source.filename,
	source.mime_type,
	source.size_bytes,
	source.sha256,
	source.s3_bucket,
	source.s3_key,
	source.downloadable,
	'session',
	session.external_id,
	session.created_by_api_key_id,
	entry.created_at
from filestore_entries entry
join filestore_filesystems filesystem
	on filesystem.uuid = entry.filesystem_uuid
	and filesystem.workspace_uuid = entry.workspace_uuid
	and filesystem.deleted_at is null
join sessions session
	on session.uuid = filesystem.session_uuid
	and session.deleted_at is null
join workspaces workspace
	on workspace.id = session.workspace_id
	and workspace.uuid = entry.workspace_uuid
join files source
	on source.uuid = entry.source_file_uuid
	and source.workspace_id = session.workspace_id
	and source.deleted_at is null
where entry.kind = 'file'
	and left(entry.path, char_length('/uploads/')) = '/uploads/'
	and entry.managed_by = 'session_file_resource'
	and entry.source_file_uuid is not null
	and entry.deleted_at is null
	and (entry.expires_at is null or entry.expires_at > now())
on conflict (uuid) do update
set filename = excluded.filename,
	mime_type = excluded.mime_type,
	size_bytes = excluded.size_bytes,
	sha256 = excluded.sha256,
	s3_bucket = excluded.s3_bucket,
	s3_key = excluded.s3_key,
	downloadable = excluded.downloadable,
	scope_type = excluded.scope_type,
	scope_id = excluded.scope_id,
	created_by_api_key_id = excluded.created_by_api_key_id,
	created_at = excluded.created_at,
	deleted_at = null;

-- +goose Down

-- 该迁移只补齐可由 Filestore 事实重新生成的投影数据。运行后无法区分迁移
-- 创建的投影与应用正常同步创建的投影，因此回滚不删除任何业务记录。
select 1;
