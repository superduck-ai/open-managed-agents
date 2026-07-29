-- +goose Up

alter table files
	add column detected_mime_type text,
	add column metadata jsonb not null default '{}'::jsonb,
	add column authorization_metadata jsonb not null default '{}'::jsonb,
	add column tags text[] not null default array[]::text[],
	add column md5 text,
	add column s3_etag text,
	add column s3_version_id text;

alter table session_resources
	drop constraint session_resources_type_check,
	alter column payload drop not null,
	add column path text,
	add column parent_path text,
	add column file_uuid uuid,
	add column skill_version_uuid uuid,
	add column expires_at timestamptz;

alter table session_resources
	add constraint session_resources_type_check check (
		resource_type in ('file', 'directory', 'skill_archive', 'github_repository', 'memory_store')
	),
	add constraint session_resources_path_check check (
		path is null
		or (
			path <> '/'
			and left(path, 1) = '/'
			and right(path, 1) <> '/'
			and position('//' in path) = 0
			and octet_length(path) <= 4096
			and path !~ '(^|/)\.{1,2}(/|$)'
		)
	) not valid,
	add constraint session_resources_parent_path_check check (
		(path is null and parent_path is null)
		or (
			path is not null
			and parent_path is not null
			and octet_length(parent_path) <= 4096
			and (
				parent_path = '/'
				or (
					left(parent_path, 1) = '/'
					and right(parent_path, 1) <> '/'
					and position('//' in parent_path) = 0
					and parent_path !~ '(^|/)\.{1,2}(/|$)'
				)
			)
		)
	) not valid;

-- 统一会删除旧 namespace 表，因此必须先证明每条仍有效的旧记录都能解析到
-- 同一租户下的 filesystem 与 Session。无外键历史库中的孤立或错配引用不能
-- 通过 INNER JOIN 静默消失。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_entries entry
		where entry.deleted_at is null
			and not exists (
				select 1
				from filestore_filesystems filesystem
				join sessions session
					on session.uuid = filesystem.session_uuid
					and session.deleted_at is null
				join workspaces workspace
					on workspace.id = session.workspace_id
					and workspace.uuid = entry.workspace_uuid
				join organizations organization
					on organization.id = session.organization_id
					and organization.uuid = entry.organization_uuid
				where filesystem.uuid = entry.filesystem_uuid
					and filesystem.workspace_uuid = entry.workspace_uuid
					and filesystem.organization_uuid = entry.organization_uuid
					and filesystem.deleted_at is null
			)
	) then
		raise exception 'cannot unify Session namespace: an active entry has invalid tenant or Session references';
	end if;

	if exists (
		select 1
		from filestore_entries entry
		where entry.kind = 'file'
			and entry.managed_by = 'session_file_resource'
			and entry.source_file_uuid is not null
			and entry.deleted_at is null
			and (
				entry.expires_at is not null
				or not exists (
					select 1
					from filestore_filesystems filesystem
					join sessions session
						on session.uuid = filesystem.session_uuid
						and session.deleted_at is null
					join session_resources resource
						on resource.uuid = entry.managed_resource_uuid
						and resource.organization_id = session.organization_id
						and resource.workspace_id = session.workspace_id
						and resource.session_id = session.id
						and resource.resource_type = 'file'
						and resource.deleted_at is null
					join files source
						on source.uuid = entry.source_file_uuid
						and source.workspace_id = session.workspace_id
						and source.deleted_at is null
					where filesystem.uuid = entry.filesystem_uuid
						and filesystem.workspace_uuid = entry.workspace_uuid
						and filesystem.organization_uuid = entry.organization_uuid
						and filesystem.deleted_at is null
				)
			)
	) then
		raise exception 'cannot unify Session namespace: an Input reference cannot be resolved';
	end if;
end $$;
-- +goose StatementEnd

-- Attached Input 保留原 Resource 身份，并接管 namespace path 与 Source File 引用。
update session_resources resource
set path = entry.path,
	parent_path = entry.parent_path,
	file_uuid = entry.source_file_uuid,
	updated_at = greatest(resource.updated_at, entry.updated_at)
from filestore_entries entry
where resource.uuid = entry.managed_resource_uuid
	and resource.resource_type = 'file'
	and resource.deleted_at is null
	and entry.kind = 'file'
	and entry.managed_by = 'session_file_resource'
	and entry.source_file_uuid is not null
	and entry.deleted_at is null
	and (entry.expires_at is null or entry.expires_at > now())
	and exists (
		select 1 from files source
		where source.uuid = entry.source_file_uuid
			and source.workspace_id = resource.workspace_id
			and source.deleted_at is null
	);

-- 所有非 Input 文件都转换为真实 File。已有 Output projection 通过 UUID 冲突原地补齐。
insert into files (
	uuid, external_id, workspace_id, filename, mime_type, detected_mime_type,
	size_bytes, metadata, authorization_metadata, tags, downloadable, md5, sha256,
	s3_bucket, s3_key, s3_etag, s3_version_id, scope_type, scope_id,
	created_by_api_key_id, created_at
)
select
	entry.uuid,
	concat('file_', replace(cast(gen_random_uuid() as text), '-', '')),
	session.workspace_id,
	regexp_replace(entry.path, '^.*/', ''),
	coalesce(nullif(entry.media_type, ''), 'application/octet-stream'),
	entry.detected_mime_type,
	entry.size_bytes,
	entry.metadata,
	entry.authorization_metadata,
	entry.tags,
	entry.downloadable,
	entry.md5,
	entry.sha256,
	entry.s3_bucket,
	entry.s3_key,
	entry.s3_etag,
	entry.s3_version_id,
	'session',
	case
		when left(entry.path, char_length('/outputs/')) = '/outputs/' then session.external_id
		else null
	end,
	coalesce(api_key.id, session.created_by_api_key_id),
	entry.created_at
from filestore_entries entry
join filestore_filesystems filesystem
	on filesystem.uuid = entry.filesystem_uuid
	and filesystem.workspace_uuid = entry.workspace_uuid
	and filesystem.deleted_at is null
join sessions session
	on session.uuid = filesystem.session_uuid
	and session.deleted_at is null
left join api_keys api_key
	on api_key.uuid = entry.created_by_api_key_uuid
	and api_key.workspace_id = session.workspace_id
where entry.kind = 'file'
	and entry.deleted_at is null
	and not (
		entry.managed_by = 'session_file_resource'
		and entry.source_file_uuid is not null
	)
on conflict (uuid) do update
set filename = excluded.filename,
	mime_type = excluded.mime_type,
	detected_mime_type = excluded.detected_mime_type,
	size_bytes = excluded.size_bytes,
	metadata = excluded.metadata,
	authorization_metadata = excluded.authorization_metadata,
	tags = excluded.tags,
	downloadable = excluded.downloadable,
	md5 = excluded.md5,
	sha256 = excluded.sha256,
	s3_bucket = excluded.s3_bucket,
	s3_key = excluded.s3_key,
	s3_etag = excluded.s3_etag,
	s3_version_id = excluded.s3_version_id,
	scope_type = excluded.scope_type,
	scope_id = excluded.scope_id,
	created_by_api_key_id = excluded.created_by_api_key_id,
	created_at = excluded.created_at,
	deleted_at = null;

-- Input 已经合并进现有 Resource；其余活动 namespace 节点创建新的 sesrsc_ Resource。
insert into session_resources (
	uuid, external_id, organization_id, workspace_id, session_id, session_external_id,
	resource_type, payload, secret_payload, path, parent_path, file_uuid,
	skill_version_uuid, expires_at, created_at, updated_at
)
select
	gen_random_uuid(),
	concat('sesrsc_', replace(cast(gen_random_uuid() as text), '-', '')),
	session.organization_id,
	session.workspace_id,
	session.id,
	session.external_id,
	case entry.kind
		when 'archive' then 'skill_archive'
		else entry.kind
	end,
	null,
	null,
	entry.path,
	entry.parent_path,
	case when entry.kind = 'file' then entry.uuid else null end,
	case when entry.kind = 'archive' then entry.managed_resource_uuid else null end,
	entry.expires_at,
	entry.created_at,
	entry.updated_at
from filestore_entries entry
join filestore_filesystems filesystem
	on filesystem.uuid = entry.filesystem_uuid
	and filesystem.workspace_uuid = entry.workspace_uuid
	and filesystem.deleted_at is null
join sessions session
	on session.uuid = filesystem.session_uuid
	and session.deleted_at is null
where entry.deleted_at is null
	and not (
		entry.kind = 'file'
		and entry.managed_by = 'session_file_resource'
		and entry.source_file_uuid is not null
	)
;

-- Input 的兼容 File 行只复制元数据，不拥有对象；统一后直接删除。
delete from files projection
using filestore_entries entry
where projection.uuid = entry.uuid
	and entry.kind = 'file'
	and entry.managed_by = 'session_file_resource'
	and entry.source_file_uuid is not null;

create unique index session_resources_session_path_active_v1_key
	on session_resources (workspace_id, session_id, path)
	where deleted_at is null and path is not null;

create index session_resources_session_parent_path_active_v1_idx
	on session_resources (workspace_id, session_id, parent_path, path, id)
	where deleted_at is null and path is not null;

create index session_resources_file_uuid_active_v1_idx
	on session_resources (workspace_id, file_uuid)
	where deleted_at is null and file_uuid is not null;

create index session_resources_public_created_v1_idx
	on session_resources (workspace_id, session_id, created_at desc, id desc)
	where deleted_at is null and payload is not null;

create index session_resources_catalog_created_v1_idx
	on session_resources (workspace_id, session_id, created_at desc, id desc)
	where deleted_at is null and resource_type = 'file' and path is not null;

create index session_resources_expires_v1_idx
	on session_resources (expires_at, id)
	where deleted_at is null and expires_at is not null;

create unique index session_resources_skill_version_active_v1_key
	on session_resources (workspace_id, session_id, skill_version_uuid)
	where deleted_at is null and skill_version_uuid is not null;

alter table session_resources
	add constraint session_resources_namespace_shape_check check (
		(
			path is null
			and parent_path is null
			and file_uuid is null
			and skill_version_uuid is null
			and expires_at is null
		)
		or (
			path is not null
			and parent_path is not null
			and (
				(resource_type = 'file' and file_uuid is not null and skill_version_uuid is null)
				or (resource_type = 'directory' and file_uuid is null and skill_version_uuid is null and expires_at is null)
				or (resource_type = 'skill_archive' and file_uuid is null and skill_version_uuid is not null and expires_at is null)
			)
		)
	) not valid,
	add constraint session_resources_internal_secret_check check (
		payload is not null or secret_payload is null
	) not valid;

alter table session_resources
	validate constraint session_resources_path_check,
	validate constraint session_resources_parent_path_check,
	validate constraint session_resources_namespace_shape_check,
	validate constraint session_resources_internal_secret_check;

drop table filestore_entries;

-- +goose Down

-- 迁移删除了旧 Entry 历史和 Input 兼容 File 行，无法无损回滚。
select 1;
