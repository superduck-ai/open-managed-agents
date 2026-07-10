-- +goose Up
create table if not exists mcp_tool_catalogs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	transport_type text not null,
	endpoint_url text not null,
	endpoint_key text not null,
	auth_scope_key text not null default 'anonymous',
	auth_scope_reference text,
	tools jsonb,
	source text,
	last_result_status text,
	protocol_version text,
	server_info jsonb,
	catalog_hash text,
	discovered_at timestamptz,
	expires_at timestamptz,
	last_attempt_at timestamptz,
	last_error_code text,
	last_error_message text,
	last_error_at timestamptz,
	retry_after timestamptz,
	requested_generation bigint not null default 0,
	settled_generation bigint not null default 0,
	last_referenced_at timestamptz not null default now(),
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint mcp_tool_catalogs_id_pk primary key (id),
	constraint mcp_tool_catalogs_uuid_key unique (uuid),
	constraint mcp_tool_catalogs_external_id_key unique (external_id),
	constraint mcp_tool_catalogs_scope_endpoint_key unique (organization_id, workspace_id, endpoint_key, auth_scope_key),
	constraint mcp_tool_catalogs_transport_type_check check (transport_type in ('url')),
	constraint mcp_tool_catalogs_tools_array_check check (tools is null or jsonb_typeof(tools) = 'array'),
	constraint mcp_tool_catalogs_source_check check (source is null or source in ('anonymous_probe', 'runtime_observation')),
	constraint mcp_tool_catalogs_result_status_check check (last_result_status is null or last_result_status in ('success', 'auth_required', 'error')),
	constraint mcp_tool_catalogs_generation_check check (
		requested_generation >= 0
		and settled_generation >= 0
		and settled_generation <= requested_generation
	)
);

create index if not exists mcp_tool_catalogs_scope_expiry_v1_idx
	on mcp_tool_catalogs (organization_id, workspace_id, expires_at);

create index if not exists mcp_tool_catalogs_scope_reference_v1_idx
	on mcp_tool_catalogs (organization_id, workspace_id, last_referenced_at);

-- +goose Down
drop table if exists mcp_tool_catalogs;
