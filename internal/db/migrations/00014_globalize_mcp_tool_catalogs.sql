-- +goose Up
-- MCP tool catalog 只保存 tools/list 的最近一次成功快照，并按规范化的
-- transport_type + endpoint_url 全局共享；组织、workspace 和 Agent 不参与 catalog identity。
create table mcp_tool_catalogs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	transport_type text not null,
	endpoint_url text not null,
	tools jsonb not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint mcp_tool_catalogs_id_pk primary key (id),
	constraint mcp_tool_catalogs_uuid_key unique (uuid),
	constraint mcp_tool_catalogs_external_id_key unique (external_id),
	constraint mcp_tool_catalogs_transport_endpoint_url_key unique (transport_type, endpoint_url),
	constraint mcp_tool_catalogs_transport_type_check check (transport_type in ('url')),
	constraint mcp_tool_catalogs_endpoint_url_length_check check (octet_length(endpoint_url) between 1 and 2048),
	constraint mcp_tool_catalogs_tools_array_check check (jsonb_typeof(tools) = 'array')
);

-- +goose Down
drop table if exists mcp_tool_catalogs;
