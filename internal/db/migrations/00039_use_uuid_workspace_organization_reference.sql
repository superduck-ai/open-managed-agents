-- +goose Up

-- Workspace 的组织归属需要在整库恢复、租户搬迁和跨库合并后保持稳定，
-- 不能依赖源数据库可重新生成的 organization identity。
-- PostgreSQL 不能原地调整列顺序，因此按最终结构重建表并保留 workspace identity。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from workspaces w
		left join organizations o on o.id = w.organization_id
		where o.id is null
	) then
		raise exception 'cannot migrate workspace organization references to UUID';
	end if;
end $$;
-- +goose StatementEnd

create table workspaces_uuid_refs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	name text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	compartment_id text not null default gen_random_uuid()::text,
	display_color text not null default '#6C5BB9',
	data_residency jsonb not null default '{"workspace_geo":"us","allowed_inference_geos":"unrestricted","default_inference_geo":"global"}'::jsonb,
	external_key_id text,
	tags jsonb not null default '{}'::jsonb,
	constraint workspaces_uuid_refs_id_pk primary key (id),
	constraint workspaces_uuid_refs_uuid_key unique (uuid),
	constraint workspaces_uuid_refs_external_id_key unique (external_id),
	constraint workspaces_uuid_refs_organization_name_key unique (organization_uuid, name)
);

insert into workspaces_uuid_refs (
	id, uuid, external_id, organization_uuid, name, created_at, updated_at,
	archived_at, compartment_id, display_color, data_residency, external_key_id, tags
)
overriding system value
select
	w.id, w.uuid, w.external_id, o.uuid, w.name, w.created_at, w.updated_at,
	w.archived_at, w.compartment_id, w.display_color, w.data_residency, w.external_key_id, w.tags
from workspaces w
join organizations o on o.id = w.organization_id;

drop table workspaces;
alter table workspaces_uuid_refs rename to workspaces;

alter table workspaces
	rename constraint workspaces_uuid_refs_id_pk to workspaces_id_pk;
alter table workspaces
	rename constraint workspaces_uuid_refs_uuid_key to workspaces_uuid_key;
alter table workspaces
	rename constraint workspaces_uuid_refs_external_id_key to workspaces_external_id_key;
alter table workspaces
	rename constraint workspaces_uuid_refs_organization_name_key to workspaces_organization_uuid_name_key;

select setval(
	pg_get_serial_sequence('workspaces', 'id'),
	coalesce((select max(id) from workspaces), 1),
	exists (select 1 from workspaces)
);

-- +goose Down

-- 回滚时仅在每条稳定 UUID 都能解析为当前库 organization identity 时恢复旧结构。
-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from workspaces w
		left join organizations o on o.uuid = w.organization_uuid
		where o.uuid is null
	) then
		raise exception 'cannot restore workspace organization internal references';
	end if;
end $$;
-- +goose StatementEnd

create table workspaces_internal_refs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	name text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	compartment_id text not null default gen_random_uuid()::text,
	display_color text not null default '#6C5BB9',
	data_residency jsonb not null default '{"workspace_geo":"us","allowed_inference_geos":"unrestricted","default_inference_geo":"global"}'::jsonb,
	external_key_id text,
	tags jsonb not null default '{}'::jsonb,
	constraint workspaces_internal_refs_id_pk primary key (id),
	constraint workspaces_internal_refs_uuid_key unique (uuid),
	constraint workspaces_internal_refs_external_id_key unique (external_id),
	constraint workspaces_internal_refs_organization_name_key unique (organization_id, name)
);

insert into workspaces_internal_refs (
	id, uuid, external_id, organization_id, name, created_at, updated_at,
	archived_at, compartment_id, display_color, data_residency, external_key_id, tags
)
overriding system value
select
	w.id, w.uuid, w.external_id, o.id, w.name, w.created_at, w.updated_at,
	w.archived_at, w.compartment_id, w.display_color, w.data_residency, w.external_key_id, w.tags
from workspaces w
join organizations o on o.uuid = w.organization_uuid;

drop table workspaces;
alter table workspaces_internal_refs rename to workspaces;

alter table workspaces
	rename constraint workspaces_internal_refs_id_pk to workspaces_id_pk;
alter table workspaces
	rename constraint workspaces_internal_refs_uuid_key to workspaces_uuid_key;
alter table workspaces
	rename constraint workspaces_internal_refs_external_id_key to workspaces_external_id_key;
alter table workspaces
	rename constraint workspaces_internal_refs_organization_name_key to workspaces_organization_name_key;

select setval(
	pg_get_serial_sequence('workspaces', 'id'),
	coalesce((select max(id) from workspaces), 1),
	exists (select 1 from workspaces)
);
