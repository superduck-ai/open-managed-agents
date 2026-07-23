-- +goose Up

-- Filestore 节点属于可跨库迁移的资源树，持久化引用不能依赖当前数据库的 identity。
-- 先完整校验旧引用及其租户链；任何孤立或错配记录都会中止迁移，避免重建表时静默丢行。
lock table filestore_entries in access exclusive mode;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_entries e
		left join organizations o on o.id = e.organization_id
		left join workspaces w on w.id = e.workspace_id
		left join filestore_filesystems fs on fs.id = e.filesystem_id
		left join api_keys ak on ak.id = e.created_by_api_key_id
		left join sessions s on s.id = e.created_by_session_id
		left join code_sessions cs on cs.id = e.created_by_code_session_id
		where o.id is null
			or w.id is null
			or w.organization_id <> o.id
			or fs.id is null
			or fs.organization_uuid <> o.uuid
			or fs.workspace_uuid <> w.uuid
			or fs.external_id <> e.filesystem_external_id
			or (e.created_by_api_key_id is not null and (
				ak.id is null or ak.workspace_id <> w.id
			))
			or (e.created_by_session_id is not null and (
				s.id is null or s.organization_id <> o.id or s.workspace_id <> w.id
			))
			or (e.created_by_code_session_id is not null and (
				cs.id is null or cs.organization_id <> o.id or cs.workspace_id <> w.id
			))
	) then
		raise exception 'cannot migrate Filestore entry references to UUID';
	end if;
end $$;
-- +goose StatementEnd

-- PostgreSQL 不能原地调整列顺序，因此直接按最终顺序重建表；UUID 引用仍位于原归属字段区域。
create table filestore_entries_uuid_refs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	filesystem_uuid uuid not null,
	kind text not null,
	path text not null,
	parent_path text,
	size_bytes bigint,
	media_type text,
	detected_mime_type text,
	metadata jsonb not null default '{}'::jsonb,
	authorization_metadata jsonb not null default '{}'::jsonb,
	tags text[] not null default array[]::text[],
	downloadable boolean not null default false,
	md5 text,
	sha256 text,
	s3_bucket text,
	s3_key text,
	s3_etag text,
	s3_version_id text,
	expires_at timestamptz,
	created_by_api_key_uuid uuid,
	created_by_session_uuid uuid,
	created_by_code_session_uuid uuid,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint filestore_entries_uuid_refs_id_pk primary key (id),
	constraint filestore_entries_uuid_refs_uuid_key unique (uuid),
	constraint filestore_entries_uuid_refs_external_id_key unique (external_id),
	constraint filestore_entries_uuid_refs_workspace_external_id_key unique (workspace_uuid, external_id),
	constraint filestore_entries_uuid_refs_kind_check check (kind in ('file', 'directory')),
	constraint filestore_entries_uuid_refs_path_check check (
		path <> '/'
		and left(path, 1) = '/'
		and right(path, 1) <> '/'
		and position('//' in path) = 0
		and octet_length(path) <= 4096
		and path !~ '(^|/)\.{1,2}(/|$)'
	),
	constraint filestore_entries_uuid_refs_parent_path_check check (
		parent_path is not null
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
	),
	constraint filestore_entries_uuid_refs_blob_shape_check check (
		(
			kind = 'directory'
			and size_bytes is null
			and media_type is null
			and detected_mime_type is null
			and md5 is null
			and sha256 is null
			and s3_bucket is null
			and s3_key is null
			and s3_etag is null
			and s3_version_id is null
			and expires_at is null
		)
		or (
			kind = 'file'
			and size_bytes is not null
			and size_bytes >= 0
			and media_type is not null
			and md5 is not null
			and char_length(md5) > 0
			and sha256 is not null
			and char_length(sha256) = 64
			and s3_bucket is not null
			and char_length(s3_bucket) > 0
			and s3_key is not null
			and char_length(s3_key) > 0
		)
	)
);

insert into filestore_entries_uuid_refs (
	id, uuid, external_id, organization_uuid, workspace_uuid, filesystem_uuid,
	kind, path, parent_path, size_bytes, media_type, detected_mime_type,
	metadata, authorization_metadata, tags, downloadable, md5, sha256,
	s3_bucket, s3_key, s3_etag, s3_version_id, expires_at,
	created_by_api_key_uuid, created_by_session_uuid, created_by_code_session_uuid,
	created_at, updated_at, deleted_at
)
overriding system value
select
	e.id, e.uuid, e.external_id, o.uuid, w.uuid, fs.uuid,
	e.kind, e.path, e.parent_path, e.size_bytes, e.media_type, e.detected_mime_type,
	e.metadata, e.authorization_metadata, e.tags, e.downloadable, e.md5, e.sha256,
	e.s3_bucket, e.s3_key, e.s3_etag, e.s3_version_id, e.expires_at,
	ak.uuid, s.uuid, cs.uuid, e.created_at, e.updated_at, e.deleted_at
from filestore_entries e
join organizations o on o.id = e.organization_id
join workspaces w on w.id = e.workspace_id and w.organization_id = o.id
join filestore_filesystems fs
	on fs.id = e.filesystem_id
	and fs.organization_uuid = o.uuid
	and fs.workspace_uuid = w.uuid
left join api_keys ak on ak.id = e.created_by_api_key_id
left join sessions s on s.id = e.created_by_session_id
left join code_sessions cs on cs.id = e.created_by_code_session_id;

drop table filestore_entries;
alter table filestore_entries_uuid_refs rename to filestore_entries;

alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_id_pk to filestore_entries_id_pk;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_uuid_key to filestore_entries_uuid_key;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_external_id_key to filestore_entries_external_id_key;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_workspace_external_id_key
		to filestore_entries_workspace_uuid_external_id_key;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_kind_check to filestore_entries_kind_check;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_path_check to filestore_entries_path_check;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_parent_path_check to filestore_entries_parent_path_check;
alter table filestore_entries
	rename constraint filestore_entries_uuid_refs_blob_shape_check to filestore_entries_blob_shape_check;

select setval(
	pg_get_serial_sequence('filestore_entries', 'id'),
	coalesce((select max(id) from filestore_entries), 1),
	exists (select 1 from filestore_entries)
);

create unique index filestore_entries_filesystem_path_active_v2_key
	on filestore_entries (workspace_uuid, filesystem_uuid, path)
	where deleted_at is null;

create index filestore_entries_filesystem_parent_path_active_v2_idx
	on filestore_entries (workspace_uuid, filesystem_uuid, parent_path, path, id)
	where deleted_at is null;

create index filestore_entries_expiry_v2_idx
	on filestore_entries (expires_at, id, filesystem_uuid)
	where deleted_at is null and kind = 'file' and expires_at is not null;

create index filestore_entries_object_key_active_v2_idx
	on filestore_entries (workspace_uuid, s3_bucket, s3_key)
	where deleted_at is null and kind = 'file';

-- +goose Down

-- 回滚同样要求每个稳定引用都能恢复为当前库内部主键；否则拒绝产生含义不明的记录。
lock table filestore_entries in access exclusive mode;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_entries e
		left join organizations o on o.uuid = e.organization_uuid
		left join workspaces w on w.uuid = e.workspace_uuid
		left join filestore_filesystems fs on fs.uuid = e.filesystem_uuid
		left join api_keys ak on ak.uuid = e.created_by_api_key_uuid
		left join sessions s on s.uuid = e.created_by_session_uuid
		left join code_sessions cs on cs.uuid = e.created_by_code_session_uuid
		where o.id is null
			or w.id is null
			or w.organization_id <> o.id
			or fs.id is null
			or fs.organization_uuid <> o.uuid
			or fs.workspace_uuid <> w.uuid
			or (e.created_by_api_key_uuid is not null and (
				ak.id is null or ak.workspace_id <> w.id
			))
			or (e.created_by_session_uuid is not null and (
				s.id is null or s.organization_id <> o.id or s.workspace_id <> w.id
			))
			or (e.created_by_code_session_uuid is not null and (
				cs.id is null or cs.organization_id <> o.id or cs.workspace_id <> w.id
			))
	) then
		raise exception 'cannot restore Filestore entry internal references';
	end if;
end $$;
-- +goose StatementEnd

create table filestore_entries_internal_refs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	filesystem_id bigint not null,
	filesystem_external_id text not null,
	kind text not null,
	path text not null,
	parent_path text,
	size_bytes bigint,
	media_type text,
	detected_mime_type text,
	metadata jsonb not null default '{}'::jsonb,
	authorization_metadata jsonb not null default '{}'::jsonb,
	tags text[] not null default array[]::text[],
	downloadable boolean not null default false,
	md5 text,
	sha256 text,
	s3_bucket text,
	s3_key text,
	s3_etag text,
	s3_version_id text,
	expires_at timestamptz,
	created_by_api_key_id bigint,
	created_by_session_id bigint,
	created_by_code_session_id bigint,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint filestore_entries_internal_refs_id_pk primary key (id),
	constraint filestore_entries_internal_refs_uuid_key unique (uuid),
	constraint filestore_entries_internal_refs_external_id_key unique (external_id),
	constraint filestore_entries_internal_refs_workspace_external_id_key unique (workspace_id, external_id),
	constraint filestore_entries_internal_refs_kind_check check (kind in ('file', 'directory')),
	constraint filestore_entries_internal_refs_path_check check (
		path <> '/'
		and left(path, 1) = '/'
		and right(path, 1) <> '/'
		and position('//' in path) = 0
		and octet_length(path) <= 4096
		and path !~ '(^|/)\.{1,2}(/|$)'
	),
	constraint filestore_entries_internal_refs_parent_path_check check (
		parent_path is not null
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
	),
	constraint filestore_entries_internal_refs_blob_shape_check check (
		(
			kind = 'directory'
			and size_bytes is null
			and media_type is null
			and detected_mime_type is null
			and md5 is null
			and sha256 is null
			and s3_bucket is null
			and s3_key is null
			and s3_etag is null
			and s3_version_id is null
			and expires_at is null
		)
		or (
			kind = 'file'
			and size_bytes is not null
			and size_bytes >= 0
			and media_type is not null
			and md5 is not null
			and char_length(md5) > 0
			and sha256 is not null
			and char_length(sha256) = 64
			and s3_bucket is not null
			and char_length(s3_bucket) > 0
			and s3_key is not null
			and char_length(s3_key) > 0
		)
	)
);

insert into filestore_entries_internal_refs (
	id, uuid, external_id, organization_id, workspace_id, filesystem_id,
	filesystem_external_id, kind, path, parent_path, size_bytes, media_type,
	detected_mime_type, metadata, authorization_metadata, tags, downloadable,
	md5, sha256, s3_bucket, s3_key, s3_etag, s3_version_id, expires_at,
	created_by_api_key_id, created_by_session_id, created_by_code_session_id,
	created_at, updated_at, deleted_at
)
overriding system value
select
	e.id, e.uuid, e.external_id, o.id, w.id, fs.id, fs.external_id,
	e.kind, e.path, e.parent_path, e.size_bytes, e.media_type, e.detected_mime_type,
	e.metadata, e.authorization_metadata, e.tags, e.downloadable, e.md5, e.sha256,
	e.s3_bucket, e.s3_key, e.s3_etag, e.s3_version_id, e.expires_at,
	ak.id, s.id, cs.id, e.created_at, e.updated_at, e.deleted_at
from filestore_entries e
join organizations o on o.uuid = e.organization_uuid
join workspaces w on w.uuid = e.workspace_uuid and w.organization_id = o.id
join filestore_filesystems fs
	on fs.uuid = e.filesystem_uuid
	and fs.organization_uuid = o.uuid
	and fs.workspace_uuid = w.uuid
left join api_keys ak on ak.uuid = e.created_by_api_key_uuid
left join sessions s on s.uuid = e.created_by_session_uuid
left join code_sessions cs on cs.uuid = e.created_by_code_session_uuid;

drop table filestore_entries;
alter table filestore_entries_internal_refs rename to filestore_entries;

alter table filestore_entries
	rename constraint filestore_entries_internal_refs_id_pk to filestore_entries_id_pk;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_uuid_key to filestore_entries_uuid_key;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_external_id_key to filestore_entries_external_id_key;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_workspace_external_id_key
		to filestore_entries_workspace_external_id_key;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_kind_check to filestore_entries_kind_check;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_path_check to filestore_entries_path_check;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_parent_path_check to filestore_entries_parent_path_check;
alter table filestore_entries
	rename constraint filestore_entries_internal_refs_blob_shape_check to filestore_entries_blob_shape_check;

select setval(
	pg_get_serial_sequence('filestore_entries', 'id'),
	coalesce((select max(id) from filestore_entries), 1),
	exists (select 1 from filestore_entries)
);

create unique index filestore_entries_filesystem_path_active_v1_key
	on filestore_entries (workspace_id, filesystem_id, path)
	where deleted_at is null;

create index filestore_entries_filesystem_parent_path_active_v1_idx
	on filestore_entries (workspace_id, filesystem_id, parent_path, path, id)
	where deleted_at is null;

create index filestore_entries_expiry_v1_idx
	on filestore_entries (expires_at, id, filesystem_id)
	where deleted_at is null and kind = 'file' and expires_at is not null;

create index filestore_entries_object_key_active_v1_idx
	on filestore_entries (workspace_id, s3_bucket, s3_key)
	where deleted_at is null and kind = 'file';
