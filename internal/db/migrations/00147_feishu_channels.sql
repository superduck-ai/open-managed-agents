-- +goose Up
create table if not exists channels (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	agent_external_id text not null,
	name text not null,
	channel_type text not null default 'feishu',
	mode text not null,
	enabled boolean not null default true,
	binding_status text not null default 'unbound',
	app_id text not null default '',
	app_secret_envelope jsonb,
	include_thinking boolean not null default false,
	include_tool_calls boolean not null default false,
	reply_in_thread boolean not null default false,
	deleted_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channels_id_pk primary key (id),
	constraint channels_uuid_key unique (uuid),
	constraint channels_external_id_key unique (external_id),
	constraint channels_mode_check check (mode in ('open', 'pairing')),
	constraint channels_binding_status_check check (binding_status in ('unbound', 'bound'))
);

create index if not exists channels_agent_v1_idx
	on channels (organization_uuid, workspace_uuid, agent_external_id, created_at desc)
	where deleted_at is null;

create unique index if not exists channels_bound_app_v1_idx
	on channels (app_id)
	where deleted_at is null and binding_status = 'bound';

create table if not exists channel_pairings (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	channel_external_id text not null,
	kind text not null,
	target text not null,
	display_name text not null default '',
	status text not null default 'pending',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_pairings_id_pk primary key (id),
	constraint channel_pairings_uuid_key unique (uuid),
	constraint channel_pairings_external_id_key unique (external_id),
	constraint channel_pairings_target_key unique (channel_external_id, kind, target),
	constraint channel_pairings_kind_check check (kind in ('user', 'chat')),
	constraint channel_pairings_status_check check (status in ('pending', 'approved', 'rejected'))
);

create index if not exists channel_pairings_channel_v1_idx
	on channel_pairings (channel_external_id, created_at);

create table if not exists channel_sessions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	channel_external_id text not null,
	scope_key text not null,
	session_external_id text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_sessions_id_pk primary key (id),
	constraint channel_sessions_uuid_key unique (uuid),
	constraint channel_sessions_scope_key unique (channel_external_id, scope_key)
);

create table if not exists channel_agent_settings (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	agent_external_id text not null,
	environment_external_id text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_agent_settings_id_pk primary key (id),
	constraint channel_agent_settings_uuid_key unique (uuid),
	constraint channel_agent_settings_scope_key unique (organization_uuid, workspace_uuid, agent_external_id)
);

-- +goose Down
drop table if exists channel_agent_settings;
drop table if exists channel_sessions;
drop table if exists channel_pairings;
drop table if exists channels;
