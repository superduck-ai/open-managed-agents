-- +goose Up

-- Filestore 的会话归属与创建者信息需要在整库恢复、租户搬迁和跨库合并后保持稳定。
-- 先通过旧内部主键回填 UUID；任何无法解析的引用都让迁移失败，避免静默丢失归属。
alter table filestore_filesystems
	add column session_uuid uuid,
	add column code_session_uuid uuid,
	add column created_by_api_key_uuid uuid;

update filestore_filesystems fs
set session_uuid = s.uuid
from sessions s
where s.id = fs.session_id
	and s.external_id = fs.session_external_id;

update filestore_filesystems fs
set code_session_uuid = cs.uuid
from code_sessions cs
where cs.id = fs.code_session_id
	and cs.external_id = fs.code_session_external_id;

update filestore_filesystems fs
set created_by_api_key_uuid = ak.uuid
from api_keys ak
where ak.id = fs.created_by_api_key_id;

-- +goose StatementBegin
do $$
begin
	if exists (select 1 from filestore_filesystems where session_uuid is null) then
		raise exception 'cannot migrate Filestore session reference to UUID';
	end if;
	if exists (
		select 1 from filestore_filesystems
		where code_session_id is not null and code_session_uuid is null
	) then
		raise exception 'cannot migrate Filestore code-session reference to UUID';
	end if;
	if exists (
		select 1 from filestore_filesystems
		where created_by_api_key_id is not null and created_by_api_key_uuid is null
	) then
		raise exception 'cannot migrate Filestore API-key reference to UUID';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists filestore_filesystems_workspace_session_active_v1_idx;
drop index if exists filestore_filesystems_workspace_code_session_active_v1_idx;

alter table filestore_filesystems
	alter column session_uuid set not null,
	drop constraint filestore_filesystems_code_session_pair_check,
	drop column session_id,
	drop column session_external_id,
	drop column code_session_id,
	drop column code_session_external_id,
	drop column created_by_api_key_id;

create index filestore_filesystems_workspace_session_active_v2_idx
	on filestore_filesystems (workspace_id, session_uuid)
	where deleted_at is null;

create index filestore_filesystems_workspace_code_session_active_v2_idx
	on filestore_filesystems (workspace_id, code_session_uuid)
	where deleted_at is null and code_session_uuid is not null;

-- +goose Down

alter table filestore_filesystems
	add column session_id bigint,
	add column session_external_id text,
	add column code_session_id bigint,
	add column code_session_external_id text,
	add column created_by_api_key_id bigint;

update filestore_filesystems fs
set session_id = s.id,
	session_external_id = s.external_id
from sessions s
where s.uuid = fs.session_uuid;

update filestore_filesystems fs
set code_session_id = cs.id,
	code_session_external_id = cs.external_id
from code_sessions cs
where cs.uuid = fs.code_session_uuid;

update filestore_filesystems fs
set created_by_api_key_id = ak.id
from api_keys ak
where ak.uuid = fs.created_by_api_key_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from filestore_filesystems
		where session_id is null or session_external_id is null
	) then
		raise exception 'cannot restore Filestore session internal reference';
	end if;
	if exists (
		select 1 from filestore_filesystems
		where code_session_uuid is not null
			and (code_session_id is null or code_session_external_id is null)
	) then
		raise exception 'cannot restore Filestore code-session internal reference';
	end if;
	if exists (
		select 1 from filestore_filesystems
		where created_by_api_key_uuid is not null and created_by_api_key_id is null
	) then
		raise exception 'cannot restore Filestore API-key internal reference';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists filestore_filesystems_workspace_session_active_v2_idx;
drop index if exists filestore_filesystems_workspace_code_session_active_v2_idx;

alter table filestore_filesystems
	alter column session_id set not null,
	alter column session_external_id set not null,
	add constraint filestore_filesystems_code_session_pair_check check (
		(code_session_id is null and code_session_external_id is null)
		or (code_session_id is not null and code_session_external_id is not null)
	),
	drop column session_uuid,
	drop column code_session_uuid,
	drop column created_by_api_key_uuid;

create index filestore_filesystems_workspace_session_active_v1_idx
	on filestore_filesystems (workspace_id, session_id)
	where deleted_at is null;

create index filestore_filesystems_workspace_code_session_active_v1_idx
	on filestore_filesystems (workspace_id, code_session_id)
	where deleted_at is null and code_session_id is not null;
