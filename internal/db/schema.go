package db

// schemaSQL is the legacy bootstrap snapshot used only by migrateLegacyTextIDSchema.
// New schema changes belong in internal/db/migrations/*.sql.
const schemaSQL = `
create extension if not exists pgcrypto;

create table if not exists organizations (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	name text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	settings jsonb not null default '{"default_workspace_settings":{"enable_api_keys":true}}'::jsonb,
	profile jsonb not null default '{}'::jsonb,
	constraint organizations_id_pk primary key (id),
	constraint organizations_uuid_key unique (uuid),
	constraint organizations_external_id_key unique (external_id)
);

alter table if exists organizations add column if not exists updated_at timestamptz not null default now();
alter table if exists organizations add column if not exists settings jsonb not null default '{"default_workspace_settings":{"enable_api_keys":true}}'::jsonb;
alter table if exists organizations add column if not exists profile jsonb not null default '{}'::jsonb;

create table if not exists workspaces (
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
	constraint workspaces_id_pk primary key (id),
	constraint workspaces_uuid_key unique (uuid),
	constraint workspaces_external_id_key unique (external_id),
	constraint workspaces_organization_name_key unique (organization_id, name)
);

alter table if exists workspaces add column if not exists updated_at timestamptz not null default now();
alter table if exists workspaces add column if not exists archived_at timestamptz;
alter table if exists workspaces add column if not exists compartment_id text not null default gen_random_uuid()::text;
alter table if exists workspaces add column if not exists display_color text not null default '#6C5BB9';
alter table if exists workspaces add column if not exists data_residency jsonb not null default '{"workspace_geo":"us","allowed_inference_geos":"unrestricted","default_inference_geo":"global"}'::jsonb;
alter table if exists workspaces add column if not exists external_key_id text;
alter table if exists workspaces add column if not exists tags jsonb not null default '{}'::jsonb;

create table if not exists api_keys (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	key_hash text not null,
	status text not null default 'active',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	created_by_user_id bigint,
	name text not null default '',
	partial_key_hint text not null default '',
	expires_at timestamptz,
	constraint api_keys_id_pk primary key (id),
	constraint api_keys_uuid_key unique (uuid),
	constraint api_keys_external_id_key unique (external_id),
	constraint api_keys_key_hash_v2_key unique (key_hash)
);

alter table if exists api_keys add column if not exists updated_at timestamptz not null default now();
alter table if exists api_keys add column if not exists created_by_user_id bigint;
alter table if exists api_keys add column if not exists name text not null default '';
alter table if exists api_keys add column if not exists partial_key_hint text not null default '';
alter table if exists api_keys add column if not exists expires_at timestamptz;

create table if not exists console_api_keys (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	api_key_uuid text not null,
	org_uuid text not null,
	workspace_id text not null default 'default',
	name text not null,
	key_prefix text not null,
	key_suffix text not null,
	key_hash text not null,
	status text not null default 'active',
	created_by_user_uuid text,
	last_used_at timestamptz,
	expires_at timestamptz,
	archived_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint console_api_keys_id_pk primary key (id),
	constraint console_api_keys_uuid_key unique (uuid),
	constraint console_api_keys_external_id_key unique (external_id),
	constraint console_api_keys_api_key_uuid_key unique (api_key_uuid),
	constraint console_api_keys_key_hash_key unique (key_hash)
);

alter table if exists console_api_keys add column if not exists uuid uuid default gen_random_uuid();
alter table if exists console_api_keys add column if not exists external_id text;
alter table if exists console_api_keys add column if not exists api_key_uuid text;
alter table if exists console_api_keys add column if not exists workspace_id text not null default 'default';
alter table if exists console_api_keys add column if not exists status text not null default 'active';
alter table if exists console_api_keys add column if not exists created_by_user_uuid text;
alter table if exists console_api_keys add column if not exists last_used_at timestamptz;
alter table if exists console_api_keys add column if not exists expires_at timestamptz;
alter table if exists console_api_keys add column if not exists archived_at timestamptz;
alter table if exists console_api_keys add column if not exists updated_at timestamptz not null default now();

update console_api_keys
set status = 'archived'
where archived_at is not null;

update console_api_keys
set uuid = gen_random_uuid()
where uuid is null;

update console_api_keys
set external_id = coalesce(nullif(external_id, ''), nullif(api_key_uuid, ''), 'api_key_' || id::text)
where external_id is null or external_id = '';

update console_api_keys
set api_key_uuid = external_id
where api_key_uuid is null or api_key_uuid = '';

alter table if exists console_api_keys alter column uuid set not null;
alter table if exists console_api_keys alter column external_id set not null;
alter table if exists console_api_keys alter column api_key_uuid set not null;

create unique index if not exists console_api_keys_uuid_key on console_api_keys (uuid);
create unique index if not exists console_api_keys_external_id_key on console_api_keys (external_id);
create unique index if not exists console_api_keys_api_key_uuid_key on console_api_keys (api_key_uuid);
create unique index if not exists console_api_keys_key_hash_key on console_api_keys (key_hash);
create index if not exists console_api_keys_org_archived_idx on console_api_keys (org_uuid, archived_at);
create index if not exists console_api_keys_org_workspace_archived_idx on console_api_keys (org_uuid, workspace_id, archived_at);
create index if not exists console_api_keys_workspace_created_idx on console_api_keys (workspace_id, created_at);

create table if not exists users (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	email text not null,
	name text not null default '',
	role text not null default 'admin',
	added_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint users_id_pk primary key (id),
	constraint users_uuid_key unique (uuid),
	constraint users_external_id_key unique (external_id),
	constraint users_role_check check (role in ('user', 'developer', 'billing', 'admin', 'claude_code_user'))
);

create unique index if not exists users_organization_email_active_v1_key
	on users (organization_id, lower(email))
	where deleted_at is null;

create index if not exists users_organization_created_v1_idx
	on users (organization_id, added_at desc, id desc)
	where deleted_at is null;

create table if not exists organization_invites (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	email text not null,
	role text not null,
	status text not null default 'pending',
	invited_at timestamptz not null default now(),
	expires_at timestamptz not null,
	deleted_at timestamptz,
	constraint organization_invites_id_pk primary key (id),
	constraint organization_invites_uuid_key unique (uuid),
	constraint organization_invites_external_id_key unique (external_id),
	constraint organization_invites_role_check check (role in ('user', 'developer', 'billing', 'admin', 'claude_code_user')),
	constraint organization_invites_status_check check (status in ('accepted', 'expired', 'deleted', 'pending'))
);

create index if not exists organization_invites_org_created_v1_idx
	on organization_invites (organization_id, invited_at desc, id desc);

create table if not exists workspace_members (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	workspace_external_id text not null,
	user_id bigint not null,
	user_external_id text not null,
	workspace_role text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint workspace_members_id_pk primary key (id),
	constraint workspace_members_uuid_key unique (uuid),
	constraint workspace_members_external_id_key unique (external_id),
	constraint workspace_members_role_check check (workspace_role in ('workspace_user', 'workspace_developer', 'workspace_restricted_developer', 'workspace_admin', 'workspace_billing'))
);

create unique index if not exists workspace_members_workspace_user_active_v1_key
	on workspace_members (workspace_id, user_id)
	where deleted_at is null;

create index if not exists workspace_members_workspace_created_v1_idx
	on workspace_members (workspace_id, created_at desc, id desc)
	where deleted_at is null;

drop table if exists platform_sessions;

create table if not exists external_keys (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	display_name text not null,
	geo text not null default 'us',
	provider_config jsonb not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint external_keys_id_pk primary key (id),
	constraint external_keys_uuid_key unique (uuid),
	constraint external_keys_external_id_key unique (external_id),
	constraint external_keys_geo_check check (geo in ('us'))
);

create index if not exists external_keys_organization_created_v1_idx
	on external_keys (organization_id, created_at desc, id desc)
	where deleted_at is null;

create table if not exists mcp_tunnels (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint,
	workspace_external_id text,
	display_name text,
	domain text not null,
	token_id text,
	tunnel_token text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	constraint mcp_tunnels_id_pk primary key (id),
	constraint mcp_tunnels_uuid_key unique (uuid),
	constraint mcp_tunnels_external_id_key unique (external_id),
	constraint mcp_tunnels_domain_key unique (domain)
);

create index if not exists mcp_tunnels_organization_created_v1_idx
	on mcp_tunnels (organization_id, created_at desc, id desc);

create table if not exists mcp_tunnel_certificates (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	tunnel_id bigint not null,
	tunnel_external_id text not null,
	ca_certificate_pem text not null,
	fingerprint text not null,
	expires_at timestamptz,
	created_at timestamptz not null default now(),
	archived_at timestamptz,
	constraint mcp_tunnel_certificates_id_pk primary key (id),
	constraint mcp_tunnel_certificates_uuid_key unique (uuid),
	constraint mcp_tunnel_certificates_external_id_key unique (external_id)
);

create index if not exists mcp_tunnel_certificates_tunnel_created_v1_idx
	on mcp_tunnel_certificates (tunnel_id, created_at desc, id desc);

create table if not exists files (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	filename text not null,
	mime_type text not null,
	size_bytes bigint not null,
	sha256 text not null,
	s3_bucket text not null,
	s3_key text not null,
	downloadable boolean not null default false,
	scope_type text,
	scope_id text,
	created_by_api_key_id bigint not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint files_id_pk primary key (id),
	constraint files_uuid_key unique (uuid),
	constraint files_external_id_key unique (external_id),
	constraint files_size_bytes_non_negative check (size_bytes >= 0)
);

create index if not exists files_workspace_created_v2_idx
	on files (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists files_workspace_scope_v2_idx
	on files (workspace_id, scope_id)
	where deleted_at is null and scope_id is not null;

create table if not exists skills (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	display_title text,
	latest_version text,
	source text not null default 'custom',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint skills_id_pk primary key (id),
	constraint skills_uuid_key unique (uuid),
	constraint skills_external_id_key unique (external_id),
	constraint skills_workspace_external_id_key unique (workspace_id, external_id),
	constraint skills_source_check check (source in ('custom'))
);

create index if not exists skills_workspace_created_v1_idx
	on skills (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create table if not exists skill_versions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	skill_id bigint not null,
	skill_external_id text not null,
	version text not null,
	name text not null,
	description text not null default '',
	directory text not null,
	s3_bucket text not null,
	s3_key text not null,
	size_bytes bigint not null,
	sha256 text not null,
	created_by_api_key_id bigint not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint skill_versions_id_pk primary key (id),
	constraint skill_versions_uuid_key unique (uuid),
	constraint skill_versions_external_id_key unique (external_id),
	constraint skill_versions_skill_version_key unique (skill_id, version),
	constraint skill_versions_size_bytes_non_negative check (size_bytes >= 0)
);

create index if not exists skill_versions_skill_created_v1_idx
	on skill_versions (skill_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists skill_versions_workspace_skill_v1_idx
	on skill_versions (workspace_id, skill_external_id, version)
	where deleted_at is null;

create table if not exists jobs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	type text not null,
	status text not null,
	payload jsonb not null default '{}'::jsonb,
	attempts integer not null default 0,
	locked_by text,
	locked_until timestamptz,
	run_after timestamptz not null default now(),
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint jobs_id_pk primary key (id),
	constraint jobs_uuid_key unique (uuid),
	constraint jobs_external_id_key unique (external_id)
);

create index if not exists jobs_ready_v2_idx
	on jobs (status, run_after, created_at);

create index if not exists jobs_type_ready_v1_idx
	on jobs (type, status, run_after, created_at);

create table if not exists webhook_endpoints (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	url text not null,
	name text not null,
	description text not null default '',
	enabled_events jsonb not null default '[]'::jsonb,
	signing_secret text not null,
	status text not null default 'enabled',
	disabled_reason text,
	consecutive_failures integer not null default 0,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint webhook_endpoints_id_pk primary key (id),
	constraint webhook_endpoints_uuid_key unique (uuid),
	constraint webhook_endpoints_external_id_key unique (external_id),
	constraint webhook_endpoints_workspace_external_id_key unique (workspace_id, external_id),
	constraint webhook_endpoints_status_check check (status in ('enabled', 'disabled')),
	constraint webhook_endpoints_consecutive_failures_non_negative check (consecutive_failures >= 0)
);

create index if not exists webhook_endpoints_workspace_created_v1_idx
	on webhook_endpoints (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists webhook_endpoints_workspace_status_v1_idx
	on webhook_endpoints (workspace_id, status, created_at desc)
	where deleted_at is null;

create table if not exists message_batches (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	api_variant text not null,
	anthropic_version text not null default '2023-06-01',
	beta_headers jsonb not null default '[]'::jsonb,
	processing_status text not null default 'in_progress',
	request_count integer not null,
	processing_count integer not null default 0,
	succeeded_count integer not null default 0,
	errored_count integer not null default 0,
	canceled_count integer not null default 0,
	expired_count integer not null default 0,
	results_s3_bucket text,
	results_s3_key text,
	results_size_bytes bigint,
	results_sha256 text,
	created_at timestamptz not null default now(),
	expires_at timestamptz not null,
	ended_at timestamptz,
	cancel_initiated_at timestamptz,
	archived_at timestamptz,
	deleted_at timestamptz,
	last_error text,
	updated_at timestamptz not null default now(),
	constraint message_batches_id_pk primary key (id),
	constraint message_batches_uuid_key unique (uuid),
	constraint message_batches_external_id_key unique (external_id),
	constraint message_batches_status_check check (
		processing_status in ('in_progress', 'canceling', 'ended')
	),
	constraint message_batches_api_variant_check check (
		api_variant in ('stable', 'beta')
	),
	constraint message_batches_request_count_positive check (request_count > 0),
	constraint message_batches_counts_non_negative check (
		processing_count >= 0
		and succeeded_count >= 0
		and errored_count >= 0
		and canceled_count >= 0
		and expired_count >= 0
	),
	constraint message_batches_results_size_non_negative check (
		results_size_bytes is null or results_size_bytes >= 0
	)
);

create index if not exists message_batches_workspace_created_v1_idx
	on message_batches (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists message_batches_workspace_status_v1_idx
	on message_batches (workspace_id, processing_status, created_at desc)
	where deleted_at is null;

create index if not exists message_batches_results_expiry_v1_idx
	on message_batches (created_at)
	where deleted_at is null and results_s3_key is not null;

create index if not exists message_batches_expiry_sweep_v1_idx
	on message_batches (expires_at)
	where deleted_at is null and processing_status in ('in_progress', 'canceling');

create table if not exists message_batch_requests (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	message_batch_id bigint not null,
	request_index integer not null,
	custom_id text not null,
	params jsonb not null,
	status text not null default 'queued',
	result jsonb,
	upstream_request_id text,
	started_at timestamptz,
	completed_at timestamptz,
	in_flight_worker_id text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint message_batch_requests_id_pk primary key (id),
	constraint message_batch_requests_uuid_key unique (uuid),
	constraint message_batch_requests_external_id_key unique (external_id),
	constraint message_batch_requests_status_check check (
		status in ('queued', 'in_flight', 'succeeded', 'errored', 'canceled', 'expired')
	),
	constraint message_batch_requests_custom_id_key unique (message_batch_id, custom_id),
	constraint message_batch_requests_index_key unique (message_batch_id, request_index),
	constraint message_batch_requests_index_non_negative check (request_index >= 0)
);

create index if not exists message_batch_requests_batch_index_v1_idx
	on message_batch_requests (message_batch_id, request_index);

create index if not exists message_batch_requests_batch_status_v1_idx
	on message_batch_requests (message_batch_id, status);

create index if not exists message_batch_requests_workspace_batch_v1_idx
	on message_batch_requests (workspace_id, message_batch_id);

create table if not exists agents (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	current_version integer not null default 1,
	name text not null,
	description text,
	system text,
	model jsonb not null,
	mcp_servers jsonb not null default '[]'::jsonb,
	metadata jsonb not null default '{}'::jsonb,
	multiagent jsonb,
	skills jsonb not null default '[]'::jsonb,
	tools jsonb not null default '[]'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint agents_id_pk primary key (id),
	constraint agents_uuid_key unique (uuid),
	constraint agents_external_id_key unique (external_id),
	constraint agents_workspace_external_id_key unique (workspace_id, external_id),
	constraint agents_current_version_positive check (current_version > 0)
);

create index if not exists agents_workspace_created_v1_idx
	on agents (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists agents_workspace_active_created_v1_idx
	on agents (workspace_id, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create table if not exists agent_versions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	workspace_id bigint not null,
	agent_id bigint not null,
	agent_external_id text not null,
	version integer not null,
	name text not null,
	description text,
	system text,
	model jsonb not null,
	mcp_servers jsonb not null default '[]'::jsonb,
	metadata jsonb not null default '{}'::jsonb,
	multiagent jsonb,
	skills jsonb not null default '[]'::jsonb,
	tools jsonb not null default '[]'::jsonb,
	agent_created_at timestamptz not null,
	agent_updated_at timestamptz not null,
	archived_at timestamptz,
	created_at timestamptz not null default now(),
	constraint agent_versions_id_pk primary key (id),
	constraint agent_versions_uuid_key unique (uuid),
	constraint agent_versions_external_id_key unique (external_id),
	constraint agent_versions_agent_version_key unique (agent_id, version),
	constraint agent_versions_workspace_agent_version_key unique (workspace_id, agent_external_id, version),
	constraint agent_versions_version_positive check (version > 0)
);

create index if not exists agent_versions_workspace_agent_version_v1_idx
	on agent_versions (workspace_id, agent_external_id, version desc, id desc);

create table if not exists environments (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	name text not null,
	description text not null default '',
	config jsonb not null default '{}'::jsonb,
	metadata jsonb not null default '{}'::jsonb,
	scope text,
	provider text not null default 'e2b',
	resolved_template text not null default 'claude-code-interpreter',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint environments_id_pk primary key (id),
	constraint environments_uuid_key unique (uuid),
	constraint environments_external_id_key unique (external_id),
	constraint environments_workspace_external_id_key unique (workspace_id, external_id),
	constraint environments_scope_check check (scope is null or scope in ('organization', 'account')),
	constraint environments_provider_check check (provider in ('e2b'))
);

create unique index if not exists environments_workspace_name_active_v1_key
	on environments (workspace_id, name)
	where deleted_at is null;

create index if not exists environments_workspace_created_v1_idx
	on environments (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists environments_workspace_active_created_v1_idx
	on environments (workspace_id, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create table if not exists environment_keys (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	environment_id bigint not null,
	environment_external_id text not null,
	key_hash text not null,
	status text not null default 'active',
	created_at timestamptz not null default now(),
	last_used_at timestamptz,
	constraint environment_keys_id_pk primary key (id),
	constraint environment_keys_uuid_key unique (uuid),
	constraint environment_keys_external_id_key unique (external_id),
	constraint environment_keys_key_hash_key unique (key_hash),
	constraint environment_keys_status_check check (status in ('active', 'revoked'))
);

create index if not exists environment_keys_environment_v1_idx
	on environment_keys (workspace_id, environment_external_id)
	where status = 'active';

create table if not exists vaults (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	display_name text not null,
	metadata jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint vaults_id_pk primary key (id),
	constraint vaults_uuid_key unique (uuid),
	constraint vaults_external_id_key unique (external_id),
	constraint vaults_workspace_external_id_key unique (workspace_id, external_id),
	constraint vaults_display_name_length check (char_length(display_name) between 1 and 255)
);

create index if not exists vaults_workspace_created_v1_idx
	on vaults (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists vaults_workspace_active_created_v1_idx
	on vaults (workspace_id, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create table if not exists vault_credentials (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	vault_id bigint not null,
	vault_external_id text not null,
	created_by_api_key_id bigint not null,
	display_name text not null,
	metadata jsonb not null default '{}'::jsonb,
	auth_type text not null,
	credential_key text not null,
	auth jsonb not null,
	secret_payload jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint vault_credentials_id_pk primary key (id),
	constraint vault_credentials_uuid_key unique (uuid),
	constraint vault_credentials_external_id_key unique (external_id),
	constraint vault_credentials_workspace_external_id_key unique (workspace_id, external_id),
	constraint vault_credentials_display_name_length check (char_length(display_name) between 1 and 255),
	constraint vault_credentials_auth_type_check check (auth_type in ('mcp_oauth', 'static_bearer', 'environment_variable'))
);

create unique index if not exists vault_credentials_vault_key_active_v1_key
	on vault_credentials (vault_id, credential_key)
	where deleted_at is null and archived_at is null;

create index if not exists vault_credentials_vault_created_v1_idx
	on vault_credentials (workspace_id, vault_external_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists vault_credentials_vault_active_created_v1_idx
	on vault_credentials (workspace_id, vault_external_id, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create table if not exists memory_stores (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	name text not null,
	description text not null default '',
	metadata jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint memory_stores_id_pk primary key (id),
	constraint memory_stores_uuid_key unique (uuid),
	constraint memory_stores_external_id_key unique (external_id),
	constraint memory_stores_workspace_external_id_key unique (workspace_id, external_id),
	constraint memory_stores_name_length check (char_length(name) between 1 and 255),
	constraint memory_stores_description_length check (char_length(description) <= 1024)
);

create index if not exists memory_stores_workspace_created_v1_idx
	on memory_stores (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists memory_stores_workspace_active_created_v1_idx
	on memory_stores (workspace_id, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create table if not exists memories (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	memory_store_id bigint not null,
	memory_store_external_id text not null,
	current_version_id bigint,
	current_version_external_id text,
	path text not null,
	content_size_bytes bigint not null,
	content_sha256 text not null,
	s3_bucket text not null,
	s3_key text not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint memories_id_pk primary key (id),
	constraint memories_uuid_key unique (uuid),
	constraint memories_external_id_key unique (external_id),
	constraint memories_workspace_external_id_key unique (workspace_id, external_id),
	constraint memories_content_size_non_negative check (content_size_bytes >= 0),
	constraint memories_content_sha256_length check (char_length(content_sha256) = 64)
);

create unique index if not exists memories_store_path_active_v1_key
	on memories (memory_store_id, path)
	where deleted_at is null;

create index if not exists memories_store_path_v1_idx
	on memories (workspace_id, memory_store_external_id, path asc, id asc)
	where deleted_at is null;

create index if not exists memories_store_created_v1_idx
	on memories (workspace_id, memory_store_external_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists memories_store_updated_v1_idx
	on memories (workspace_id, memory_store_external_id, updated_at desc, id desc)
	where deleted_at is null;

create table if not exists memory_versions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	memory_store_id bigint not null,
	memory_store_external_id text not null,
	memory_id bigint not null,
	memory_external_id text not null,
	operation text not null,
	path text,
	content_size_bytes bigint,
	content_sha256 text,
	s3_bucket text,
	s3_key text,
	created_by_actor_type text not null,
	created_by_api_key_id bigint,
	created_by_api_key_external_id text,
	created_by_session_id text,
	created_by_user_id text,
	redacted_at timestamptz,
	redacted_by_actor_type text,
	redacted_by_api_key_id bigint,
	redacted_by_api_key_external_id text,
	redacted_by_session_id text,
	redacted_by_user_id text,
	created_at timestamptz not null default now(),
	constraint memory_versions_id_pk primary key (id),
	constraint memory_versions_uuid_key unique (uuid),
	constraint memory_versions_external_id_key unique (external_id),
	constraint memory_versions_workspace_external_id_key unique (workspace_id, external_id),
	constraint memory_versions_operation_check check (operation in ('created', 'modified', 'deleted')),
	constraint memory_versions_created_actor_type_check check (created_by_actor_type in ('api_actor', 'session_actor', 'user_actor')),
	constraint memory_versions_redacted_actor_type_check check (redacted_by_actor_type is null or redacted_by_actor_type in ('api_actor', 'session_actor', 'user_actor')),
	constraint memory_versions_content_size_non_negative check (content_size_bytes is null or content_size_bytes >= 0),
	constraint memory_versions_content_sha256_length check (content_sha256 is null or char_length(content_sha256) = 64),
	constraint memory_versions_deleted_content_null check (
		operation <> 'deleted'
		or (content_size_bytes is null and content_sha256 is null and s3_bucket is null and s3_key is null)
	)
);

create index if not exists memory_versions_store_created_v1_idx
	on memory_versions (workspace_id, memory_store_external_id, created_at desc, id desc);

create index if not exists memory_versions_memory_created_v1_idx
	on memory_versions (workspace_id, memory_store_external_id, memory_external_id, created_at desc, id desc);

create index if not exists memory_versions_store_operation_created_v1_idx
	on memory_versions (workspace_id, memory_store_external_id, operation, created_at desc, id desc);

create index if not exists memory_versions_store_api_key_created_v1_idx
	on memory_versions (workspace_id, memory_store_external_id, created_by_api_key_external_id, created_at desc, id desc)
	where created_by_api_key_external_id is not null;

create index if not exists memory_versions_store_session_created_v1_idx
	on memory_versions (workspace_id, memory_store_external_id, created_by_session_id, created_at desc, id desc)
	where created_by_session_id is not null;

create table if not exists sessions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	environment_id bigint not null,
	environment_external_id text not null,
	agent_id bigint not null,
	agent_external_id text not null,
	agent_version integer not null,
	agent_snapshot jsonb not null,
	deployment_id text,
	title text,
	metadata jsonb not null default '{}'::jsonb,
	vault_ids jsonb not null default '[]'::jsonb,
	status text not null default 'idle',
	usage jsonb not null default '{}'::jsonb,
	stats jsonb not null default '{}'::jsonb,
	outcome_evaluations jsonb not null default '[]'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint sessions_id_pk primary key (id),
	constraint sessions_uuid_key unique (uuid),
	constraint sessions_external_id_key unique (external_id),
	constraint sessions_workspace_external_id_key unique (workspace_id, external_id),
	constraint sessions_agent_version_positive check (agent_version > 0),
	constraint sessions_status_check check (status in ('rescheduling', 'running', 'idle', 'terminated'))
);

create index if not exists sessions_workspace_created_v1_idx
	on sessions (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists sessions_workspace_status_created_v1_idx
	on sessions (workspace_id, status, created_at desc, id desc)
	where deleted_at is null;

create index if not exists sessions_workspace_agent_created_v1_idx
	on sessions (workspace_id, agent_external_id, agent_version, created_at desc, id desc)
	where deleted_at is null;

create index if not exists sessions_workspace_deployment_created_v1_idx
	on sessions (workspace_id, deployment_id, created_at desc, id desc)
	where deleted_at is null and deployment_id is not null;

create table if not exists deployments (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	environment_id bigint not null,
	environment_external_id text not null,
	agent_id bigint not null,
	agent_external_id text not null,
	agent_version integer not null,
	agent_snapshot jsonb not null,
	name text not null,
	description text,
	metadata jsonb not null default '{}'::jsonb,
	initial_events jsonb not null default '[]'::jsonb,
	resources jsonb not null default '[]'::jsonb,
	resource_secrets jsonb not null default '{}'::jsonb,
	vault_ids jsonb not null default '[]'::jsonb,
	schedule jsonb,
	last_run_at timestamptz,
	status text not null default 'active',
	paused_reason jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint deployments_id_pk primary key (id),
	constraint deployments_uuid_key unique (uuid),
	constraint deployments_external_id_key unique (external_id),
	constraint deployments_workspace_external_id_key unique (workspace_id, external_id),
	constraint deployments_agent_version_positive check (agent_version > 0),
	constraint deployments_status_check check (status in ('active', 'paused'))
);

create index if not exists deployments_workspace_created_v1_idx
	on deployments (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists deployments_workspace_active_created_v1_idx
	on deployments (workspace_id, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create index if not exists deployments_workspace_status_created_v1_idx
	on deployments (workspace_id, status, created_at desc, id desc)
	where deleted_at is null and archived_at is null;

create index if not exists deployments_workspace_agent_created_v1_idx
	on deployments (workspace_id, agent_external_id, created_at desc, id desc)
	where deleted_at is null;

create table if not exists deployment_runs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	created_by_api_key_id bigint not null,
	deployment_id bigint not null,
	deployment_external_id text not null,
	agent_id bigint not null,
	agent_external_id text not null,
	agent_version integer not null,
	agent_snapshot jsonb not null,
	session_external_id text,
	error jsonb,
	trigger_type text not null,
	trigger_context jsonb not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint deployment_runs_id_pk primary key (id),
	constraint deployment_runs_uuid_key unique (uuid),
	constraint deployment_runs_external_id_key unique (external_id),
	constraint deployment_runs_workspace_external_id_key unique (workspace_id, external_id),
	constraint deployment_runs_agent_version_positive check (agent_version > 0),
	constraint deployment_runs_trigger_type_check check (trigger_type in ('manual', 'schedule')),
	constraint deployment_runs_result_check check ((session_external_id is null) <> (error is null))
);

create index if not exists deployment_runs_workspace_created_v1_idx
	on deployment_runs (workspace_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists deployment_runs_workspace_deployment_created_v1_idx
	on deployment_runs (workspace_id, deployment_external_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists deployment_runs_workspace_trigger_created_v1_idx
	on deployment_runs (workspace_id, trigger_type, created_at desc, id desc)
	where deleted_at is null;

create index if not exists deployment_runs_workspace_error_created_v1_idx
	on deployment_runs (workspace_id, (error is not null), created_at desc, id desc)
	where deleted_at is null;

create table if not exists session_threads (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	session_id bigint not null,
	session_external_id text not null,
	parent_thread_id bigint,
	parent_thread_external_id text,
	agent_snapshot jsonb not null,
	status text not null default 'idle',
	usage jsonb not null default '{}'::jsonb,
	stats jsonb not null default '{}'::jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	deleted_at timestamptz,
	constraint session_threads_id_pk primary key (id),
	constraint session_threads_uuid_key unique (uuid),
	constraint session_threads_external_id_key unique (external_id),
	constraint session_threads_workspace_external_id_key unique (workspace_id, external_id),
	constraint session_threads_status_check check (status in ('rescheduling', 'running', 'idle', 'terminated'))
);

create index if not exists session_threads_session_created_v1_idx
	on session_threads (workspace_id, session_external_id, created_at desc, id desc)
	where deleted_at is null;

create table if not exists session_resources (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	session_id bigint not null,
	session_external_id text not null,
	resource_type text not null,
	payload jsonb not null default '{}'::jsonb,
	secret_payload jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint session_resources_id_pk primary key (id),
	constraint session_resources_uuid_key unique (uuid),
	constraint session_resources_external_id_key unique (external_id),
	constraint session_resources_workspace_external_id_key unique (workspace_id, external_id),
	constraint session_resources_type_check check (resource_type in ('file', 'github_repository', 'memory_store'))
);

create index if not exists session_resources_session_created_v1_idx
	on session_resources (workspace_id, session_external_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists session_resources_memory_store_v1_idx
	on session_resources (workspace_id, (payload->>'memory_store_id'))
	where deleted_at is null and resource_type = 'memory_store';

create table if not exists session_events (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	session_id bigint not null,
	session_external_id text not null,
	thread_id bigint,
	thread_external_id text,
	event_type text not null,
	payload jsonb not null,
	processed_at timestamptz not null,
	created_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint session_events_id_pk primary key (id),
	constraint session_events_uuid_key unique (uuid),
	constraint session_events_external_id_key unique (external_id),
	constraint session_events_workspace_external_id_key unique (workspace_id, external_id)
);

create index if not exists session_events_session_created_v1_idx
	on session_events (workspace_id, session_external_id, created_at asc, id asc)
	where deleted_at is null;

create index if not exists session_events_thread_created_v1_idx
	on session_events (workspace_id, session_external_id, thread_external_id, created_at asc, id asc)
	where deleted_at is null and thread_external_id is not null;

create index if not exists session_events_type_created_v1_idx
	on session_events (workspace_id, event_type, created_at asc, id asc)
	where deleted_at is null;

create table if not exists code_sessions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	session_id bigint not null,
	session_external_id text not null,
	environment_id bigint not null,
	environment_external_id text not null,
	work_dir text not null default '',
	permission_mode text not null default '',
	model text not null default '',
	status text not null default 'active',
	metadata jsonb not null default '{}'::jsonb,
	connection_status text not null default 'disconnected',
	last_inbound_sequence_num bigint not null default 0,
	last_outbound_sequence_num bigint not null default 0,
	last_worker_connected_at timestamptz,
	last_worker_activity_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint code_sessions_id_pk primary key (id),
	constraint code_sessions_uuid_key unique (uuid),
	constraint code_sessions_external_id_key unique (external_id),
	constraint code_sessions_workspace_external_id_key unique (workspace_id, external_id)
);

create index if not exists code_sessions_public_session_v1_idx
	on code_sessions (workspace_id, session_external_id)
	where deleted_at is null;

create table if not exists code_session_inbound_events (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	code_session_id bigint not null,
	code_session_external_id text not null,
	sequence_num bigint not null,
	event_type text not null,
	event_subtype text not null default '',
	payload_uuid text,
	request_id text,
	payload jsonb not null,
	payload_hash text not null,
	idempotency_key text not null default '',
	delivery_status text not null default 'queued',
	source text not null default '',
	sent_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint code_session_inbound_events_id_pk primary key (id),
	constraint code_session_inbound_events_uuid_key unique (uuid),
	constraint code_session_inbound_events_external_id_key unique (external_id),
	constraint code_session_inbound_events_workspace_external_id_key unique (workspace_id, external_id)
);

create unique index if not exists code_session_inbound_events_sequence_v1_key
	on code_session_inbound_events (workspace_id, code_session_external_id, sequence_num)
	where deleted_at is null;

create unique index if not exists code_session_inbound_events_idempotency_v1_key
	on code_session_inbound_events (workspace_id, idempotency_key)
	where deleted_at is null and idempotency_key <> '';

create index if not exists code_session_inbound_events_queued_v1_idx
	on code_session_inbound_events (workspace_id, code_session_external_id, sequence_num asc)
	where deleted_at is null and delivery_status = 'queued';

create table if not exists code_session_outbound_events (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	code_session_id bigint not null,
	code_session_external_id text not null,
	sequence_num bigint not null,
	event_type text not null,
	event_subtype text not null default '',
	payload_uuid text,
	request_id text,
	payload jsonb not null,
	payload_hash text not null,
	idempotency_key text not null default '',
	source text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint code_session_outbound_events_id_pk primary key (id),
	constraint code_session_outbound_events_uuid_key unique (uuid),
	constraint code_session_outbound_events_external_id_key unique (external_id),
	constraint code_session_outbound_events_workspace_external_id_key unique (workspace_id, external_id)
);

create unique index if not exists code_session_outbound_events_sequence_v1_key
	on code_session_outbound_events (workspace_id, code_session_external_id, sequence_num)
	where deleted_at is null;

create unique index if not exists code_session_outbound_events_idempotency_v1_key
	on code_session_outbound_events (workspace_id, idempotency_key)
	where deleted_at is null and idempotency_key <> '';

create index if not exists code_session_outbound_events_created_v1_idx
	on code_session_outbound_events (workspace_id, code_session_external_id, created_at asc, id asc)
	where deleted_at is null;

create table if not exists environment_work (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	environment_id bigint not null,
	environment_external_id text not null,
	data jsonb not null,
	metadata jsonb not null default '{}'::jsonb,
	secret text,
	state text not null default 'queued',
	claimed_by_worker_id text,
	claim_expires_at timestamptz,
	acknowledged_at timestamptz,
	started_at timestamptz,
	latest_heartbeat_at timestamptz,
	heartbeat_ttl_seconds integer,
	stop_requested_at timestamptz,
	stopped_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	deleted_at timestamptz,
	constraint environment_work_id_pk primary key (id),
	constraint environment_work_uuid_key unique (uuid),
	constraint environment_work_external_id_key unique (external_id),
	constraint environment_work_workspace_external_id_key unique (workspace_id, external_id),
	constraint environment_work_state_check check (state in ('queued', 'starting', 'active', 'stopping', 'stopped')),
	constraint environment_work_heartbeat_ttl_positive check (heartbeat_ttl_seconds is null or heartbeat_ttl_seconds > 0)
);

create index if not exists environment_work_environment_created_v1_idx
	on environment_work (workspace_id, environment_external_id, created_at desc, id desc)
	where deleted_at is null;

create index if not exists environment_work_poll_v1_idx
	on environment_work (workspace_id, environment_external_id, state, created_at, id)
	where deleted_at is null;

create table if not exists environment_worker_polls (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_id bigint not null,
	workspace_id bigint not null,
	environment_id bigint not null,
	environment_external_id text not null,
	worker_id text not null,
	last_poll_at timestamptz not null default now(),
	constraint environment_worker_polls_id_pk primary key (id),
	constraint environment_worker_polls_uuid_key unique (uuid),
	constraint environment_worker_polls_environment_worker_key unique (environment_id, worker_id)
);

create table if not exists environment_sandboxes (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	environment_id bigint not null,
	environment_external_id text not null,
	work_id bigint,
	work_external_id text,
	provider text not null default 'e2b',
	template text not null,
	provider_sandbox_id text,
	state text not null default 'creating',
	metadata jsonb not null default '{}'::jsonb,
	last_error text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	stopped_at timestamptz,
	constraint environment_sandboxes_id_pk primary key (id),
	constraint environment_sandboxes_uuid_key unique (uuid),
	constraint environment_sandboxes_external_id_key unique (external_id),
	constraint environment_sandboxes_provider_check check (provider in ('e2b')),
	constraint environment_sandboxes_state_check check (state in ('creating', 'running', 'stopping', 'stopped', 'failed'))
);

create index if not exists environment_sandboxes_work_v1_idx
	on environment_sandboxes (workspace_id, work_external_id)
	where work_external_id is not null;

create table if not exists workbench_prompts (
	id bigint generated always as identity,
	org_uuid text not null,
	prompt_uuid text not null,
	workspace_id text not null default 'default',
	name text not null default '',
	is_shared_with_workspace boolean not null default false,
	latest_revision_uuid text,
	deleted_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint workbench_prompts_id_pk primary key (id)
);

create unique index if not exists workbench_prompts_org_prompt_key
	on workbench_prompts (org_uuid, prompt_uuid);

create index if not exists idx_workbench_prompts_org_deleted
	on workbench_prompts (org_uuid, deleted_at);

create table if not exists workbench_prompt_revisions (
	id bigint generated always as identity,
	org_uuid text not null,
	prompt_uuid text not null,
	revision_uuid text not null,
	payload jsonb not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint workbench_prompt_revisions_id_pk primary key (id)
);

create unique index if not exists workbench_prompt_revisions_org_prompt_revision_key
	on workbench_prompt_revisions (org_uuid, prompt_uuid, revision_uuid);

create index if not exists idx_workbench_prompt_revisions_org_prompt_created
	on workbench_prompt_revisions (org_uuid, prompt_uuid, created_at desc, id desc);

create table if not exists workbench_prompt_kv (
	id bigint generated always as identity,
	org_uuid text not null,
	prompt_uuid text not null,
	key text not null,
	value text not null,
	version jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint workbench_prompt_kv_id_pk primary key (id)
);

create unique index if not exists workbench_prompt_kv_org_prompt_key_key
	on workbench_prompt_kv (org_uuid, prompt_uuid, key);

create table if not exists workbench_evaluations (
	id bigint generated always as identity,
	org_uuid text not null,
	revision_uuid text not null,
	evaluation_uuid text not null,
	payload jsonb not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint workbench_evaluations_id_pk primary key (id)
);

create unique index if not exists workbench_evaluations_org_evaluation_key
	on workbench_evaluations (org_uuid, evaluation_uuid);

create index if not exists idx_workbench_evaluations_org_revision_created
	on workbench_evaluations (org_uuid, revision_uuid, created_at asc, id asc);

create table if not exists workbench_generated_test_cases (
	id bigint generated always as identity,
	org_uuid text not null,
	values jsonb not null,
	created_at timestamptz not null default now(),
	constraint workbench_generated_test_cases_id_pk primary key (id)
);

create index if not exists idx_workbench_generated_test_cases_org_id
	on workbench_generated_test_cases (org_uuid, id asc);
`
