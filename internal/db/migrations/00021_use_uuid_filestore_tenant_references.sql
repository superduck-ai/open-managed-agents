-- +goose Up

-- Filestore 文件系统的租户归属必须能跨数据库恢复与迁移，不能依赖源库 identity。
-- 组织和工作区在同一次回填中校验归属链，防止把历史上的错配关系固化为 UUID。
alter table filestore_filesystems
	add column organization_uuid uuid,
	add column workspace_uuid uuid;

update filestore_filesystems fs
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid
from workspaces w
join organizations o on o.id = w.organization_id
where w.id = fs.workspace_id
	and o.id = fs.organization_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_filesystems
		where organization_uuid is null or workspace_uuid is null
	) then
		raise exception 'cannot migrate Filestore tenant references to UUID';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists filestore_filesystems_workspace_session_active_v2_idx;
drop index if exists filestore_filesystems_workspace_code_session_active_v2_idx;

alter table filestore_filesystems
	drop constraint filestore_filesystems_workspace_external_id_key,
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	drop column organization_id,
	drop column workspace_id,
	add constraint filestore_filesystems_workspace_uuid_external_id_key
		unique (workspace_uuid, external_id);

create index filestore_filesystems_workspace_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, session_uuid)
	where deleted_at is null;

create index filestore_filesystems_workspace_code_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, code_session_uuid)
	where deleted_at is null and code_session_uuid is not null;

-- +goose Down

alter table filestore_filesystems
	add column organization_id bigint,
	add column workspace_id bigint;

update filestore_filesystems fs
set organization_id = o.id,
	workspace_id = w.id
from workspaces w
join organizations o on o.id = w.organization_id
where w.uuid = fs.workspace_uuid
	and o.uuid = fs.organization_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from filestore_filesystems
		where organization_id is null or workspace_id is null
	) then
		raise exception 'cannot restore Filestore tenant internal references';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists filestore_filesystems_workspace_session_active_v3_idx;
drop index if exists filestore_filesystems_workspace_code_session_active_v3_idx;

alter table filestore_filesystems
	drop constraint filestore_filesystems_workspace_uuid_external_id_key,
	alter column organization_id set not null,
	alter column workspace_id set not null,
	drop column organization_uuid,
	drop column workspace_uuid,
	add constraint filestore_filesystems_workspace_external_id_key
		unique (workspace_id, external_id);

create index filestore_filesystems_workspace_session_active_v2_idx
	on filestore_filesystems (workspace_id, session_uuid)
	where deleted_at is null;

create index filestore_filesystems_workspace_code_session_active_v2_idx
	on filestore_filesystems (workspace_id, code_session_uuid)
	where deleted_at is null and code_session_uuid is not null;
