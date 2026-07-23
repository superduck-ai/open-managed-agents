-- +goose Up

-- 文件系统记录承载会话归属；根目录由该记录虚拟投影，不在 entries 中重复落表。
create table if not exists filestore_filesystems (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	session_id bigint not null,
	session_external_id text not null,
	code_session_id bigint,
	code_session_external_id text,
	created_by_api_key_id bigint,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint filestore_filesystems_id_pk primary key (id),
	constraint filestore_filesystems_uuid_key unique (uuid),
	constraint filestore_filesystems_workspace_external_id_key unique (workspace_id, external_id),
	constraint filestore_filesystems_code_session_pair_check check (
		(code_session_id is null and code_session_external_id is null)
		or (code_session_id is not null and code_session_external_id is not null)
	)
);

create index if not exists filestore_filesystems_workspace_session_active_v1_idx
	on filestore_filesystems (workspace_id, session_id)
	where deleted_at is null;

create index if not exists filestore_filesystems_workspace_code_session_active_v1_idx
	on filestore_filesystems (workspace_id, code_session_id)
	where deleted_at is null and code_session_id is not null;

create table if not exists filestore_entries (
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
	constraint filestore_entries_id_pk primary key (id),
	constraint filestore_entries_uuid_key unique (uuid),
	constraint filestore_entries_external_id_key unique (external_id),
	constraint filestore_entries_workspace_external_id_key unique (workspace_id, external_id),
	constraint filestore_entries_kind_check check (kind in ('file', 'directory')),
	-- 路径必须已经规范化；数据库约束作为绕过 HTTP/服务层写入时的最后防线。
	constraint filestore_entries_path_check check (
		path <> '/'
		and left(path, 1) = '/'
		and right(path, 1) <> '/'
		and position('//' in path) = 0
		and octet_length(path) <= 4096
		and path !~ '(^|/)\.{1,2}(/|$)'
	),
	constraint filestore_entries_parent_path_check check (
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
	constraint filestore_entries_blob_shape_check check (
		-- 目录不持有对象字段；文件必须具备配额、摘要与对象定位所需的最小信息。
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

-- 软删除记录可留作审计，但任一时刻同一路径只能有一个活动节点。
create unique index if not exists filestore_entries_filesystem_path_active_v1_key
	on filestore_entries (workspace_id, filesystem_id, path)
	where deleted_at is null;

create index if not exists filestore_entries_filesystem_parent_path_active_v1_idx
	on filestore_entries (workspace_id, filesystem_id, parent_path, path, id)
	where deleted_at is null;

create index if not exists filestore_entries_expiry_v1_idx
	on filestore_entries (expires_at, id, filesystem_id)
	where deleted_at is null and kind = 'file' and expires_at is not null;

create index if not exists filestore_entries_object_key_active_v1_idx
	on filestore_entries (workspace_id, s3_bucket, s3_key)
	where deleted_at is null and kind = 'file';

-- +goose Down

drop table if exists filestore_entries;
drop table if exists filestore_filesystems;
