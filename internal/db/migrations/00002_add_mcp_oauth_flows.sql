-- +goose Up
create table if not exists mcp_oauth_flows (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_id bigint not null,
	workspace_id bigint not null,
	vault_id bigint not null,
	vault_external_id text not null,
	user_id bigint,
	user_external_id text,
	platform_session_external_id text,
	mcp_server_url text not null,
	redirect_url text not null,
	display_name text not null,
	source text not null default '',
	authorization_endpoint text not null,
	token_endpoint text not null,
	registration_endpoint text,
	issuer text,
	resource text not null,
	scope text,
	client_id text not null,
	client_secret text,
	token_endpoint_auth_method text not null default 'none',
	code_verifier text not null,
	code_challenge_method text not null,
	status text not null default 'pending',
	credential_external_id text,
	error_code text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	expires_at timestamptz not null,
	completed_at timestamptz,
	constraint mcp_oauth_flows_id_pk primary key (id),
	constraint mcp_oauth_flows_uuid_key unique (uuid),
	constraint mcp_oauth_flows_external_id_key unique (external_id),
	constraint mcp_oauth_flows_status_check check (status in ('pending', 'completed', 'failed'))
);

create index if not exists mcp_oauth_flows_pending_v1_idx
	on mcp_oauth_flows (external_id, expires_at)
	where status = 'pending';

-- +goose Down
drop table if exists mcp_oauth_flows;
