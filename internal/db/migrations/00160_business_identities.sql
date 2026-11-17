-- +goose Up
alter table channels drop constraint channels_binding_status_check;
alter table channels add constraint channels_binding_status_check check (binding_status in ('unbound', 'bound', 'expired'));

alter table sessions add column identity_uuid uuid;
create index sessions_identity_v1_idx on sessions (workspace_uuid, identity_uuid) where identity_uuid is not null and deleted_at is null;

create table identities (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	namespace text not null default '',
	external_subject_id text not null,
	display_name text not null,
	enabled boolean not null default true,
	auto_memory_store boolean not null default false,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint identities_id_pk primary key (id),
	constraint identities_uuid_key unique (uuid),
	constraint identities_external_id_key unique (external_id)
);

create unique index identities_subject_v1_idx
	on identities (organization_uuid, workspace_uuid, namespace, external_subject_id)
	where deleted_at is null;

create index identities_workspace_v1_idx
	on identities (workspace_uuid, external_id desc)
	where deleted_at is null;

create table identity_configs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	identity_uuid uuid not null,
	agent_external_id text not null,
	revision bigint not null default 1,
	status text not null default 'active',
	env jsonb not null default '{}'::jsonb,
	vault_mounts jsonb not null default '[]'::jsonb,
	disabled_vaults jsonb not null default '[]'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint identity_configs_id_pk primary key (id),
	constraint identity_configs_uuid_key unique (uuid),
	constraint identity_configs_external_id_key unique (external_id),
	constraint identity_configs_status_check check (status in ('active', 'archived'))
);

create unique index identity_configs_active_v1_idx
	on identity_configs (workspace_uuid, identity_uuid, agent_external_id)
	where status = 'active';

create table identity_actor_links (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	channel_type text not null,
	app_id text not null,
	actor_id text not null,
	identity_uuid uuid not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint identity_actor_links_id_pk primary key (id),
	constraint identity_actor_links_uuid_key unique (uuid),
	constraint identity_actor_links_actor_key unique (organization_uuid, workspace_uuid, channel_type, app_id, actor_id)
);

create table channel_execution_contexts (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	channel_uuid uuid not null,
	channel_external_id text not null,
	mode text not null,
	identity_uuid uuid,
	agent_version integer,
	revision bigint not null default 1,
	connection_status text not null default 'unknown',
	entry_closed boolean not null default false,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_execution_contexts_id_pk primary key (id),
	constraint channel_execution_contexts_uuid_key unique (uuid),
	constraint channel_execution_contexts_channel_key unique (channel_uuid),
	constraint channel_execution_contexts_mode_check check (mode in ('fixed', 'pairing')),
	constraint channel_execution_contexts_connection_check check (connection_status in ('unknown', 'healthy', 'disconnected'))
);

create table channel_scope_pairings (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	channel_uuid uuid not null,
	channel_external_id text not null,
	scope_kind text not null,
	platform_account text not null,
	remote_id text not null,
	display_name text not null default '',
	status text not null default 'pending',
	identity_uuid uuid,
	agent_external_id text not null default '',
	agent_version integer,
	revision bigint not null default 0,
	pairing_code_hash text,
	pairing_code_expires_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint channel_scope_pairings_id_pk primary key (id),
	constraint channel_scope_pairings_uuid_key unique (uuid),
	constraint channel_scope_pairings_external_id_key unique (external_id),
	constraint channel_scope_pairings_kind_check check (scope_kind in ('direct', 'room')),
	constraint channel_scope_pairings_status_check check (status in ('pending', 'active', 'unbound'))
);

create unique index channel_scope_pairings_open_v1_idx
	on channel_scope_pairings (channel_uuid, scope_kind, platform_account, remote_id)
	where status in ('pending', 'active');

create index channel_scope_pairings_workspace_v1_idx
	on channel_scope_pairings (workspace_uuid, external_id desc);

create table session_identity_bindings (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	session_uuid uuid not null,
	session_external_id text not null,
	identity_uuid uuid not null,
	agent_external_id text not null,
	binding_revision bigint not null,
	config_revision bigint not null default 0,
	personal_vault_external_id text not null default '',
	scope_kind text not null,
	scope_key text not null,
	channel_external_id text not null default '',
	created_at timestamptz not null default now(),
	constraint session_identity_bindings_id_pk primary key (id),
	constraint session_identity_bindings_uuid_key unique (uuid),
	constraint session_identity_bindings_session_key unique (session_uuid)
);

create index session_identity_bindings_identity_v1_idx
	on session_identity_bindings (workspace_uuid, identity_uuid, created_at desc);

create table identity_cleanup_runs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	identity_uuid uuid not null,
	state text not null,
	result jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint identity_cleanup_runs_id_pk primary key (id),
	constraint identity_cleanup_runs_uuid_key unique (uuid),
	constraint identity_cleanup_runs_external_id_key unique (external_id),
	constraint identity_cleanup_runs_state_check check (state in ('queued', 'conflict', 'completed', 'partial'))
);

-- +goose Down
drop table if exists identity_cleanup_runs;
drop table if exists session_identity_bindings;
drop table if exists channel_scope_pairings;
drop table if exists channel_execution_contexts;
drop table if exists identity_actor_links;
drop table if exists identity_configs;
drop table if exists identities;
drop index if exists sessions_identity_v1_idx;
alter table sessions drop column if exists identity_uuid;
alter table channels drop constraint channels_binding_status_check;
alter table channels add constraint channels_binding_status_check check (binding_status in ('unbound', 'bound'));
