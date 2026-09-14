-- +goose Up

alter table session_resources
	add column github_repository_url text,
	add column github_repository_checkout jsonb,
	add column mount_path text,
	add column memory_store_uuid uuid,
	add column memory_access text,
	add column memory_description text,
	add column memory_instructions text,
	add column memory_name text;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from session_resources resource
		join files file
			on file.uuid = resource.file_uuid
			and file.workspace_uuid = resource.workspace_uuid
		where resource.resource_type = 'file'
			and resource.file_ownership = 'referenced'
			and (
				jsonb_typeof(resource.payload) is distinct from 'object'
				or coalesce(resource.payload->>'id', '') <> resource.external_id
				or resource.payload->>'type' is distinct from 'file'
				or coalesce(resource.payload->>'file_id', '') <> file.external_id
				or resource.payload->>'source' is distinct from '/uploads'
				or coalesce(resource.payload->>'mount_path', '') = ''
				or resource.path <> case
					when left(resource.payload->>'mount_path', char_length('/uploads/')) = '/uploads/'
						then resource.payload->>'mount_path'
					else concat('/uploads', resource.payload->>'mount_path')
				end
			)
	) then
		raise exception 'cannot remove Session Resource payload: a referenced File payload is invalid';
	end if;

	if exists (
		select 1
		from session_resources resource
		where resource.resource_type = 'github_repository'
			and (
				jsonb_typeof(resource.payload) is distinct from 'object'
				or coalesce(resource.payload->>'url', '') = ''
				or coalesce(resource.payload->>'mount_path', '') = ''
				or (resource.payload ? 'checkout' and resource.payload->'checkout' = 'null'::jsonb)
			)
	) then
		raise exception 'cannot remove Session Resource payload: a GitHub Repository payload is invalid';
	end if;

	if exists (
		select 1
		from session_resources resource
		where resource.resource_type = 'memory_store'
			and (
				jsonb_typeof(resource.payload) is distinct from 'object'
				or coalesce(resource.payload->>'memory_store_id', '') = ''
				or not exists (
					select 1
					from memory_stores memory_store
					where memory_store.workspace_uuid = resource.workspace_uuid
						and memory_store.external_id = resource.payload->>'memory_store_id'
				)
				or exists (
					select 1
					from jsonb_each(resource.payload) field
					where field.key in ('access', 'description', 'instructions', 'mount_path', 'name')
						and jsonb_typeof(field.value) not in ('string', 'null')
				)
			)
	) then
		raise exception 'cannot remove Session Resource payload: a Memory Store payload or reference is invalid';
	end if;

	if exists (
		select 1
		from session_resources
		where secret_payload is not null
			and resource_type <> 'github_repository'
	) then
		raise exception 'cannot remove Session Resource payload: a non-GitHub Resource carries a secret payload';
	end if;
end $$;
-- +goose StatementEnd

update session_resources
set mount_path = case
	when file_ownership = 'referenced' then payload->>'mount_path'
	else path
end
where resource_type = 'file'
	and path is not null;

update session_resources
set github_repository_url = payload->>'url',
	github_repository_checkout = payload->'checkout',
	mount_path = payload->>'mount_path'
where resource_type = 'github_repository';

update session_resources resource
set memory_store_uuid = memory_store.uuid,
	memory_access = resource.payload->>'access',
	memory_description = resource.payload->>'description',
	memory_instructions = resource.payload->>'instructions',
	mount_path = resource.payload->>'mount_path',
	memory_name = resource.payload->>'name'
from memory_stores memory_store
where resource.resource_type = 'memory_store'
	and memory_store.workspace_uuid = resource.workspace_uuid
	and memory_store.external_id = resource.payload->>'memory_store_id';

drop index if exists session_resources_public_created_v1_idx;
drop index if exists session_resources_memory_store_v2_idx;

alter table session_resources
	drop constraint if exists session_resources_internal_secret_check;

alter table session_resources
	add constraint session_resources_explicit_config_check check (
		(
			resource_type = 'file'
			and github_repository_url is null
			and github_repository_checkout is null
			and mount_path is not null
			and (file_ownership is distinct from 'owned' or mount_path = path)
			and memory_store_uuid is null
			and memory_access is null
			and memory_description is null
			and memory_instructions is null
			and memory_name is null
		)
		or (
			resource_type in ('directory', 'skill_archive')
			and github_repository_url is null
			and github_repository_checkout is null
			and mount_path is null
			and memory_store_uuid is null
			and memory_access is null
			and memory_description is null
			and memory_instructions is null
			and memory_name is null
		)
		or (
			resource_type = 'github_repository'
			and coalesce(github_repository_url, '') <> ''
			and coalesce(mount_path, '') <> ''
			and memory_store_uuid is null
			and memory_access is null
			and memory_description is null
			and memory_instructions is null
			and memory_name is null
		)
		or (
			resource_type = 'memory_store'
			and github_repository_url is null
			and github_repository_checkout is null
			and memory_store_uuid is not null
		)
	) not valid,
	add constraint session_resources_internal_secret_check check (
		secret_payload is null or resource_type = 'github_repository'
	) not valid;

alter table session_resources
	validate constraint session_resources_explicit_config_check,
	validate constraint session_resources_internal_secret_check;

create index session_resources_public_created_v2_idx
	on session_resources (workspace_uuid, session_uuid, created_at desc, id desc)
	where deleted_at is null
		and resource_type in ('file', 'github_repository', 'memory_store');

create index session_resources_memory_store_v3_idx
	on session_resources (workspace_uuid, memory_store_uuid)
	where deleted_at is null and resource_type = 'memory_store';

alter table session_resources
	drop column payload;

-- +goose Down

alter table session_resources
	add column payload jsonb;

update session_resources resource
set payload = jsonb_build_object(
	'id', resource.external_id,
	'type', 'file',
	'file_id', file.external_id,
	'source', '/uploads',
	'mount_path', resource.mount_path
)
from files file
where resource.resource_type = 'file'
	and resource.file_ownership = 'referenced'
	and resource.deleted_at is null
	and file.uuid = resource.file_uuid
	and file.workspace_uuid = resource.workspace_uuid;

update session_resources
set payload = jsonb_strip_nulls(jsonb_build_object(
	'id', external_id,
	'type', 'github_repository',
	'url', github_repository_url,
	'mount_path', mount_path,
	'checkout', github_repository_checkout
))
where resource_type = 'github_repository';

update session_resources resource
set payload = jsonb_strip_nulls(jsonb_build_object(
	'id', resource.external_id,
	'type', 'memory_store',
	'memory_store_id', memory_store.external_id,
	'access', resource.memory_access,
	'description', resource.memory_description,
	'instructions', resource.memory_instructions,
	'mount_path', resource.mount_path,
	'name', resource.memory_name
))
from memory_stores memory_store
where resource.resource_type = 'memory_store'
	and memory_store.uuid = resource.memory_store_uuid
	and memory_store.workspace_uuid = resource.workspace_uuid;

drop index if exists session_resources_public_created_v2_idx;
drop index if exists session_resources_memory_store_v3_idx;

alter table session_resources
	drop constraint if exists session_resources_explicit_config_check,
	drop constraint if exists session_resources_internal_secret_check;

alter table session_resources
	add constraint session_resources_internal_secret_check check (
		payload is not null or secret_payload is null
	) not valid;

alter table session_resources
	validate constraint session_resources_internal_secret_check;

create index session_resources_public_created_v1_idx
	on session_resources (workspace_uuid, session_uuid, created_at desc, id desc)
	where deleted_at is null and payload is not null;

create index session_resources_memory_store_v2_idx
	on session_resources (workspace_uuid, (payload->>'memory_store_id'))
	where deleted_at is null and resource_type = 'memory_store';

alter table session_resources
	drop column github_repository_url,
	drop column github_repository_checkout,
	drop column mount_path,
	drop column memory_store_uuid,
	drop column memory_access,
	drop column memory_description,
	drop column memory_instructions,
	drop column memory_name;
