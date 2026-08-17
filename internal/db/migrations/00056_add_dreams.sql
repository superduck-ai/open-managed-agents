-- +goose Up
-- Dreams: asynchronous jobs that distill an input memory store plus 1~100
-- sessions into a new output memory store. This migration creates only the
-- data model and lifecycle columns; the distillation workflow (validation,
-- deduplication, reorganization, output store write) ships later and advances
-- the row through markDreamRunning/markDreamSucceeded etc.
--
-- Project rules: no foreign keys. Cross-table references use target resource
-- uuid columns and integrity is enforced by application write paths, not
-- constraints.

create table dreams (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	created_by_api_key_uuid uuid not null,
	input_store_uuid uuid not null,
	session_ids jsonb not null,
	instructions text,
	model text not null,
	status text not null default 'pending',
	output_store_uuid uuid,
	error text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	constraint dreams_id_pk primary key (id),
	constraint dreams_uuid_key unique (uuid),
	constraint dreams_external_id_key unique (external_id),
	constraint dreams_workspace_external_id_key unique (workspace_uuid, external_id)
);

create index if not exists dreams_workspace_created_v1_idx
	on dreams (workspace_uuid, created_at desc, id desc);

create index if not exists dreams_workspace_active_created_v1_idx
	on dreams (workspace_uuid, created_at desc, id desc)
	where archived_at is null;

-- +goose Down

drop table dreams;
