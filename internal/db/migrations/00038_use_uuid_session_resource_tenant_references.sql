-- +goose Up

-- Session Resource 已成为可跨库恢复的 Session namespace 事实，租户引用必须使用
-- 稳定 UUID，而不能依赖当前数据库的 identity。先验证完整租户链，再按最终字段
-- 顺序重建表，避免把替换列追加到表尾。
lock table session_resources in access exclusive mode;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from session_resources resource
		where not exists (
			select 1
			from organizations organization
			join workspaces workspace
				on workspace.organization_id = organization.id
			join sessions session
				on session.organization_id = organization.id
				and session.workspace_id = workspace.id
			where organization.id = resource.organization_id
				and workspace.id = resource.workspace_id
				and session.id = resource.session_id
		)
	) then
		raise exception 'cannot migrate Session Resource tenant references to UUID';
	end if;
end $$;
-- +goose StatementEnd

create table session_resources_uuid_swap (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	session_uuid uuid not null,
	session_external_id text not null,
	resource_type text not null,
	payload jsonb default '{}'::jsonb,
	secret_payload jsonb,
	path text,
	parent_path text,
	file_uuid uuid,
	expires_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint session_resources_uuid_swap_id_pk primary key (id),
	constraint session_resources_uuid_swap_uuid_key unique (uuid),
	constraint session_resources_uuid_swap_external_id_key unique (external_id),
	constraint session_resources_uuid_swap_workspace_external_id_key unique (workspace_uuid, external_id),
	constraint session_resources_uuid_swap_type_check check (
		resource_type in ('file', 'directory', 'skill_archive', 'github_repository', 'memory_store')
	),
	constraint session_resources_uuid_swap_path_check check (
		path is null
		or (
			path <> '/'
			and left(path, 1) = '/'
			and right(path, 1) <> '/'
			and position('//' in path) = 0
			and octet_length(path) <= 4096
			and path !~ '(^|/)\.{1,2}(/|$)'
		)
	),
	constraint session_resources_uuid_swap_parent_path_check check (
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
	),
	constraint session_resources_uuid_swap_namespace_shape_check check (
		(
			path is null
			and parent_path is null
			and file_uuid is null
			and expires_at is null
		)
		or (
			path is not null
			and parent_path is not null
			and (
				(resource_type = 'file' and file_uuid is not null)
				or (resource_type = 'directory' and file_uuid is null and expires_at is null)
				or (
					resource_type = 'skill_archive'
					and expires_at is null
					and (file_uuid is not null or deleted_at is not null)
				)
			)
		)
	),
	constraint session_resources_uuid_swap_internal_secret_check check (
		payload is not null or secret_payload is null
	)
);

insert into session_resources_uuid_swap (
	id, uuid, external_id, organization_uuid, workspace_uuid, session_uuid,
	session_external_id, resource_type, payload, secret_payload, path,
	parent_path, file_uuid, expires_at, created_at, updated_at, deleted_at
) overriding system value
select resource.id, resource.uuid, resource.external_id,
	organization.uuid, workspace.uuid, session.uuid,
	resource.session_external_id, resource.resource_type, resource.payload,
	resource.secret_payload, resource.path, resource.parent_path,
	resource.file_uuid, resource.expires_at, resource.created_at,
	resource.updated_at, resource.deleted_at
from session_resources resource
join organizations organization on organization.id = resource.organization_id
join workspaces workspace
	on workspace.id = resource.workspace_id
	and workspace.organization_id = organization.id
join sessions session
	on session.id = resource.session_id
	and session.organization_id = organization.id
	and session.workspace_id = workspace.id;

select setval(
	pg_get_serial_sequence('session_resources_uuid_swap', 'id'),
	coalesce((select max(id) from session_resources_uuid_swap), 1),
	exists (select 1 from session_resources_uuid_swap)
);

drop table session_resources;
alter table session_resources_uuid_swap rename to session_resources;
alter sequence session_resources_uuid_swap_id_seq rename to session_resources_id_seq;
alter table session_resources
	rename constraint session_resources_uuid_swap_id_pk to session_resources_id_pk;
alter table session_resources
	rename constraint session_resources_uuid_swap_uuid_key to session_resources_uuid_key;
alter table session_resources
	rename constraint session_resources_uuid_swap_external_id_key to session_resources_external_id_key;
alter table session_resources
	rename constraint session_resources_uuid_swap_workspace_external_id_key to session_resources_workspace_uuid_external_id_key;
alter table session_resources
	rename constraint session_resources_uuid_swap_type_check to session_resources_type_check;
alter table session_resources
	rename constraint session_resources_uuid_swap_path_check to session_resources_path_check;
alter table session_resources
	rename constraint session_resources_uuid_swap_parent_path_check to session_resources_parent_path_check;
alter table session_resources
	rename constraint session_resources_uuid_swap_namespace_shape_check to session_resources_namespace_shape_check;
alter table session_resources
	rename constraint session_resources_uuid_swap_internal_secret_check to session_resources_internal_secret_check;

create index session_resources_session_created_v1_idx
	on session_resources (workspace_uuid, session_external_id, created_at desc, id desc)
	where deleted_at is null;

create index session_resources_memory_store_v1_idx
	on session_resources (workspace_uuid, (payload->>'memory_store_id'))
	where deleted_at is null and resource_type = 'memory_store';

create unique index session_resources_session_path_active_v1_key
	on session_resources (workspace_uuid, session_uuid, path)
	where deleted_at is null and path is not null;

create index session_resources_session_parent_path_active_v1_idx
	on session_resources (workspace_uuid, session_uuid, parent_path, path, id)
	where deleted_at is null and path is not null;

create index session_resources_file_uuid_active_v1_idx
	on session_resources (workspace_uuid, file_uuid)
	where deleted_at is null and file_uuid is not null;

create index session_resources_owned_file_uuid_v1_idx
	on session_resources (workspace_uuid, file_uuid)
	where file_uuid is not null and payload is null and resource_type = 'file';

create index session_resources_public_created_v1_idx
	on session_resources (workspace_uuid, session_uuid, created_at desc, id desc)
	where deleted_at is null and payload is not null;

create index session_resources_catalog_created_v1_idx
	on session_resources (workspace_uuid, session_uuid, created_at desc, id desc)
	where deleted_at is null and resource_type = 'file' and path is not null;

create index session_resources_expires_v1_idx
	on session_resources (expires_at, id)
	where deleted_at is null and expires_at is not null;

-- +goose Down

lock table session_resources in access exclusive mode;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from session_resources resource
		where not exists (
			select 1
			from organizations organization
			join workspaces workspace
				on workspace.organization_id = organization.id
			join sessions session
				on session.organization_id = organization.id
				and session.workspace_id = workspace.id
			where organization.uuid = resource.organization_uuid
				and workspace.uuid = resource.workspace_uuid
				and session.uuid = resource.session_uuid
		)
	) then
		raise exception 'cannot restore Session Resource internal tenant references';
	end if;
end $$;
-- +goose StatementEnd

create table session_resources_id_swap (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	session_id bigint not null,
	session_external_id text not null,
	resource_type text not null,
	payload jsonb default '{}'::jsonb,
	secret_payload jsonb,
	path text,
	parent_path text,
	file_uuid uuid,
	expires_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint session_resources_id_swap_id_pk primary key (id),
	constraint session_resources_id_swap_uuid_key unique (uuid),
	constraint session_resources_id_swap_external_id_key unique (external_id),
	constraint session_resources_id_swap_workspace_external_id_key unique (workspace_id, external_id),
	constraint session_resources_id_swap_type_check check (
		resource_type in ('file', 'directory', 'skill_archive', 'github_repository', 'memory_store')
	),
	constraint session_resources_id_swap_path_check check (
		path is null
		or (
			path <> '/'
			and left(path, 1) = '/'
			and right(path, 1) <> '/'
			and position('//' in path) = 0
			and octet_length(path) <= 4096
			and path !~ '(^|/)\.{1,2}(/|$)'
		)
	),
	constraint session_resources_id_swap_parent_path_check check (
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
	),
	constraint session_resources_id_swap_namespace_shape_check check (
		(
			path is null
			and parent_path is null
			and file_uuid is null
			and expires_at is null
		)
		or (
			path is not null
			and parent_path is not null
			and (
				(resource_type = 'file' and file_uuid is not null)
				or (resource_type = 'directory' and file_uuid is null and expires_at is null)
				or (
					resource_type = 'skill_archive'
					and expires_at is null
					and (file_uuid is not null or deleted_at is not null)
				)
			)
		)
	),
	constraint session_resources_id_swap_internal_secret_check check (
		payload is not null or secret_payload is null
	)
);

insert into session_resources_id_swap (
	id, uuid, external_id, organization_id, workspace_id, session_id,
	session_external_id, resource_type, payload, secret_payload, path,
	parent_path, file_uuid, expires_at, created_at, updated_at, deleted_at
) overriding system value
select resource.id, resource.uuid, resource.external_id,
	organization.id, workspace.id, session.id,
	resource.session_external_id, resource.resource_type, resource.payload,
	resource.secret_payload, resource.path, resource.parent_path,
	resource.file_uuid, resource.expires_at, resource.created_at,
	resource.updated_at, resource.deleted_at
from session_resources resource
join organizations organization on organization.uuid = resource.organization_uuid
join workspaces workspace
	on workspace.uuid = resource.workspace_uuid
	and workspace.organization_id = organization.id
join sessions session
	on session.uuid = resource.session_uuid
	and session.organization_id = organization.id
	and session.workspace_id = workspace.id;

select setval(
	pg_get_serial_sequence('session_resources_id_swap', 'id'),
	coalesce((select max(id) from session_resources_id_swap), 1),
	exists (select 1 from session_resources_id_swap)
);

drop table session_resources;
alter table session_resources_id_swap rename to session_resources;
alter sequence session_resources_id_swap_id_seq rename to session_resources_id_seq;
alter table session_resources
	rename constraint session_resources_id_swap_id_pk to session_resources_id_pk;
alter table session_resources
	rename constraint session_resources_id_swap_uuid_key to session_resources_uuid_key;
alter table session_resources
	rename constraint session_resources_id_swap_external_id_key to session_resources_external_id_key;
alter table session_resources
	rename constraint session_resources_id_swap_workspace_external_id_key to session_resources_workspace_external_id_key;
alter table session_resources
	rename constraint session_resources_id_swap_type_check to session_resources_type_check;
alter table session_resources
	rename constraint session_resources_id_swap_path_check to session_resources_path_check;
alter table session_resources
	rename constraint session_resources_id_swap_parent_path_check to session_resources_parent_path_check;
alter table session_resources
	rename constraint session_resources_id_swap_namespace_shape_check to session_resources_namespace_shape_check;
alter table session_resources
	rename constraint session_resources_id_swap_internal_secret_check to session_resources_internal_secret_check;

create index session_resources_session_created_v1_idx
	on session_resources (workspace_id, session_external_id, created_at desc, id desc)
	where deleted_at is null;

create index session_resources_memory_store_v1_idx
	on session_resources (workspace_id, (payload->>'memory_store_id'))
	where deleted_at is null and resource_type = 'memory_store';

create unique index session_resources_session_path_active_v1_key
	on session_resources (workspace_id, session_id, path)
	where deleted_at is null and path is not null;

create index session_resources_session_parent_path_active_v1_idx
	on session_resources (workspace_id, session_id, parent_path, path, id)
	where deleted_at is null and path is not null;

create index session_resources_file_uuid_active_v1_idx
	on session_resources (workspace_id, file_uuid)
	where deleted_at is null and file_uuid is not null;

create index session_resources_owned_file_uuid_v1_idx
	on session_resources (workspace_id, file_uuid)
	where file_uuid is not null and payload is null and resource_type = 'file';

create index session_resources_public_created_v1_idx
	on session_resources (workspace_id, session_id, created_at desc, id desc)
	where deleted_at is null and payload is not null;

create index session_resources_catalog_created_v1_idx
	on session_resources (workspace_id, session_id, created_at desc, id desc)
	where deleted_at is null and resource_type = 'file' and path is not null;

create index session_resources_expires_v1_idx
	on session_resources (expires_at, id)
	where deleted_at is null and expires_at is not null;
