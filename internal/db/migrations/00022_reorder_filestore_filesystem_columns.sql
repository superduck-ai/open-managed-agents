-- +goose Up

-- PostgreSQL 不能原地调整列顺序，因此在同一事务中重建表。
-- Filestore 不使用外键；复制时显式保留内部 ID，随后校准新表的 identity 序列。
create table filestore_filesystems_reordered (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	session_uuid uuid not null,
	code_session_uuid uuid,
	created_by_api_key_uuid uuid,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint filestore_filesystems_reordered_id_pk primary key (id),
	constraint filestore_filesystems_reordered_uuid_key unique (uuid),
	constraint filestore_filesystems_reordered_workspace_external_id_key
		unique (workspace_uuid, external_id)
);

insert into filestore_filesystems_reordered (
	id, uuid, external_id, organization_uuid, workspace_uuid,
	session_uuid, code_session_uuid, created_by_api_key_uuid,
	created_at, updated_at, deleted_at
)
overriding system value
select
	id, uuid, external_id, organization_uuid, workspace_uuid,
	session_uuid, code_session_uuid, created_by_api_key_uuid,
	created_at, updated_at, deleted_at
from filestore_filesystems;

drop table filestore_filesystems;
alter table filestore_filesystems_reordered rename to filestore_filesystems;

alter table filestore_filesystems
	rename constraint filestore_filesystems_reordered_id_pk to filestore_filesystems_id_pk;
alter table filestore_filesystems
	rename constraint filestore_filesystems_reordered_uuid_key to filestore_filesystems_uuid_key;
alter table filestore_filesystems
	rename constraint filestore_filesystems_reordered_workspace_external_id_key
		to filestore_filesystems_workspace_uuid_external_id_key;

select setval(
	pg_get_serial_sequence('filestore_filesystems', 'id'),
	coalesce((select max(id) from filestore_filesystems), 1),
	exists (select 1 from filestore_filesystems)
);

create index filestore_filesystems_workspace_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, session_uuid)
	where deleted_at is null;

create index filestore_filesystems_workspace_code_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, code_session_uuid)
	where deleted_at is null and code_session_uuid is not null;

-- +goose Down

-- 恢复 00021 执行后的物理列顺序；字段及其业务语义保持不变。
create table filestore_filesystems_unordered (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	session_uuid uuid not null,
	code_session_uuid uuid,
	created_by_api_key_uuid uuid,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	constraint filestore_filesystems_unordered_id_pk primary key (id),
	constraint filestore_filesystems_unordered_uuid_key unique (uuid),
	constraint filestore_filesystems_unordered_workspace_external_id_key
		unique (workspace_uuid, external_id)
);

insert into filestore_filesystems_unordered (
	id, uuid, external_id, created_at, updated_at, deleted_at,
	session_uuid, code_session_uuid, created_by_api_key_uuid,
	organization_uuid, workspace_uuid
)
overriding system value
select
	id, uuid, external_id, created_at, updated_at, deleted_at,
	session_uuid, code_session_uuid, created_by_api_key_uuid,
	organization_uuid, workspace_uuid
from filestore_filesystems;

drop table filestore_filesystems;
alter table filestore_filesystems_unordered rename to filestore_filesystems;

alter table filestore_filesystems
	rename constraint filestore_filesystems_unordered_id_pk to filestore_filesystems_id_pk;
alter table filestore_filesystems
	rename constraint filestore_filesystems_unordered_uuid_key to filestore_filesystems_uuid_key;
alter table filestore_filesystems
	rename constraint filestore_filesystems_unordered_workspace_external_id_key
		to filestore_filesystems_workspace_uuid_external_id_key;

select setval(
	pg_get_serial_sequence('filestore_filesystems', 'id'),
	coalesce((select max(id) from filestore_filesystems), 1),
	exists (select 1 from filestore_filesystems)
);

create index filestore_filesystems_workspace_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, session_uuid)
	where deleted_at is null;

create index filestore_filesystems_workspace_code_session_active_v3_idx
	on filestore_filesystems (workspace_uuid, code_session_uuid)
	where deleted_at is null and code_session_uuid is not null;
