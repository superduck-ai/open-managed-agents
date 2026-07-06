-- +goose Up
alter table code_sessions
	add column if not exists last_internal_sequence_num bigint not null default 0;

create table if not exists code_session_internal_events (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	code_session_id bigint not null,
	code_session_external_id text not null,
	sequence_num bigint not null,
	event_type text not null,
	payload_uuid text not null,
	agent_id text,
	is_compaction boolean not null default false,
	payload jsonb not null,
	payload_hash text not null,
	idempotency_key text not null default '',
	event_metadata jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint code_session_internal_events_id_pk primary key (id),
	constraint code_session_internal_events_uuid_key unique (uuid),
	constraint code_session_internal_events_external_id_key unique (external_id),
	constraint code_session_internal_events_workspace_external_id_key unique (workspace_id, external_id)
);

create unique index if not exists code_session_internal_events_sequence_v1_key
	on code_session_internal_events (workspace_id, code_session_external_id, sequence_num)
	where deleted_at is null;

create unique index if not exists code_session_internal_events_idempotency_v1_key
	on code_session_internal_events (workspace_id, idempotency_key)
	where deleted_at is null and idempotency_key <> '';

create index if not exists code_session_internal_events_foreground_list_v1_idx
	on code_session_internal_events (workspace_id, code_session_external_id, sequence_num asc)
	where deleted_at is null and agent_id is null;

create index if not exists code_session_internal_events_subagent_list_v1_idx
	on code_session_internal_events (workspace_id, code_session_external_id, agent_id, sequence_num asc)
	where deleted_at is null and agent_id is not null;

create index if not exists code_session_internal_events_foreground_compaction_v1_idx
	on code_session_internal_events (workspace_id, code_session_external_id, sequence_num desc)
	where deleted_at is null and agent_id is null and is_compaction;

create index if not exists code_session_internal_events_subagent_compaction_v1_idx
	on code_session_internal_events (workspace_id, code_session_external_id, agent_id, sequence_num desc)
	where deleted_at is null and agent_id is not null and is_compaction;

-- +goose Down
drop index if exists code_session_internal_events_subagent_compaction_v1_idx;
drop index if exists code_session_internal_events_foreground_compaction_v1_idx;
drop index if exists code_session_internal_events_subagent_list_v1_idx;
drop index if exists code_session_internal_events_foreground_list_v1_idx;
drop index if exists code_session_internal_events_idempotency_v1_key;
drop index if exists code_session_internal_events_sequence_v1_key;

drop table if exists code_session_internal_events;

alter table code_sessions
	drop column if exists last_internal_sequence_num;
