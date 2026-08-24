-- +goose Up

alter table session_resources
	add column if not exists file_ownership text;

-- 在仍可使用 PR #190 既有 payload 判别时完成一次性 preflight。
-- 任一行无法同时满足 namespace、租户、Files 身份与生命周期不变量时，
-- 迁移必须整体失败，不能把冲突数据猜成 referenced 或 owned。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from session_resources resource
		where resource.resource_type = 'file'
			and resource.path is not null
			and (
				resource.parent_path is null
				or resource.file_uuid is null
				or not exists (
					select 1
					from files file
					where file.uuid = resource.file_uuid
						and file.workspace_uuid = resource.workspace_uuid
						and (resource.deleted_at is not null or file.deleted_at is null)
				)
			)
	) then
		raise exception 'cannot add Session file ownership: a namespace File has an invalid File or tenant reference';
	end if;

	if exists (
		select 1
		from session_resources resource
		join files file
			on file.uuid = resource.file_uuid
			and file.workspace_uuid = resource.workspace_uuid
		where resource.resource_type = 'file'
			and resource.path is not null
			and resource.payload is not null
			and (
				left(resource.path, char_length('/uploads/')) <> '/uploads/'
				or resource.expires_at is not null
				or coalesce(resource.payload->>'id', '') <> resource.external_id
				or resource.payload->>'type' is distinct from 'file'
				or coalesce(resource.payload->>'file_id', '') <> file.external_id
				or resource.payload->>'source' is distinct from '/uploads'
				or coalesce(resource.payload->>'mount_path', '') = ''
				or left(resource.payload->>'mount_path', 1) <> '/'
				or resource.path <> case
					when left(resource.payload->>'mount_path', char_length('/uploads/')) = '/uploads/'
						then resource.payload->>'mount_path'
					else concat('/uploads', resource.payload->>'mount_path')
				end
			)
	) then
		raise exception 'cannot add Session file ownership: a referenced File payload or namespace path is inconsistent';
	end if;

	if exists (
		select 1
		from session_resources resource
		join files file
			on file.uuid = resource.file_uuid
			and file.workspace_uuid = resource.workspace_uuid
		where resource.resource_type = 'file'
			and resource.path is not null
			and resource.payload is null
			and (
				left(resource.path, char_length('/uploads/')) = '/uploads/'
				or file.scope_type is distinct from 'session'
				or file.scope_id is distinct from resource.session_external_id
			)
	) then
		raise exception 'cannot add Session file ownership: an owned File has an inconsistent scope or uploads path';
	end if;

	if exists (
		select resource.workspace_uuid, resource.file_uuid
		from session_resources resource
		where resource.resource_type = 'file'
			and resource.path is not null
			and resource.payload is null
		group by resource.workspace_uuid, resource.file_uuid
		having count(*) > 1
	) then
		raise exception 'cannot add Session file ownership: a File has more than one owned Resource';
	end if;

	if exists (
		select 1
		from session_resources reference
		where reference.resource_type = 'file'
			and reference.path is not null
			and reference.payload is not null
			and exists (
				select 1
				from session_resources owner
				where owner.workspace_uuid = reference.workspace_uuid
					and owner.file_uuid = reference.file_uuid
					and owner.uuid <> reference.uuid
					and (
						(owner.resource_type = 'file' and owner.path is not null and owner.payload is null)
						or owner.resource_type = 'skill_archive'
					)
			)
	) then
		raise exception 'cannot add Session file ownership: a referenced File is owned by another namespace Resource';
	end if;
end $$;
-- +goose StatementEnd

update session_resources
set file_ownership = case
	when payload is not null then 'referenced'
	else 'owned'
end
where resource_type = 'file'
	and path is not null;

alter table session_resources
	drop constraint if exists session_resources_namespace_shape_check;

drop index if exists session_resources_owned_file_uuid_v1_idx;

create unique index if not exists session_resources_owned_file_uuid_v2_key
	on session_resources (workspace_uuid, file_uuid)
	where resource_type = 'file' and file_ownership = 'owned';

alter table session_resources
	drop constraint if exists session_resources_file_ownership_check;

alter table session_resources
	add constraint session_resources_file_ownership_check check (
		file_ownership is null or file_ownership in ('referenced', 'owned')
	) not valid,
	add constraint session_resources_namespace_shape_check check (
		(
			path is null
			and parent_path is null
			and file_uuid is null
			and file_ownership is null
			and expires_at is null
		)
		or (
			path is not null
			and parent_path is not null
			and (
				(
					resource_type = 'file'
					and file_uuid is not null
					and file_ownership is not null
					and file_ownership in ('referenced', 'owned')
					and (file_ownership <> 'referenced' or expires_at is null)
				)
				or (
					resource_type = 'directory'
					and file_uuid is null
					and file_ownership is null
					and expires_at is null
				)
				or (
					resource_type = 'skill_archive'
					and file_ownership is null
					and expires_at is null
					and (file_uuid is not null or deleted_at is not null)
				)
			)
		)
	) not valid;

alter table session_resources
	validate constraint session_resources_file_ownership_check,
	validate constraint session_resources_namespace_shape_check;

-- +goose Down

-- payload 在本迁移后不再承载 ownership，无法从公开内容无损重建该列。
-- +goose StatementBegin
do $$
begin
	raise exception 'migration 00052 cannot be reversed safely because file ownership is no longer encoded by payload';
end $$;
-- +goose StatementEnd
