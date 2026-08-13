-- +goose Up

create table mcp_servers (
    id bigint generated always as identity primary key,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    organization_uuid uuid not null,
    workspace_uuid uuid not null,
    name text not null,
    transport_type text not null,
    endpoint_url text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    archived_at timestamptz,
    deleted_at timestamptz,
    constraint mcp_servers_uuid_unique unique (uuid),
    constraint mcp_servers_external_id_unique unique (external_id),
    constraint mcp_servers_name_not_empty check (length(name) between 1 and 255),
    constraint mcp_servers_transport_type_check check (transport_type = 'url'),
    constraint mcp_servers_endpoint_url_not_empty check (octet_length(endpoint_url) between 1 and 2048)
);

create unique index mcp_servers_workspace_name_unique
    on mcp_servers (workspace_uuid, name)
    where deleted_at is null;

create unique index mcp_servers_workspace_endpoint_unique
    on mcp_servers (workspace_uuid, transport_type, endpoint_url)
    where deleted_at is null;

create index mcp_servers_workspace_created_page
    on mcp_servers (workspace_uuid, created_at desc, uuid desc)
    where deleted_at is null;

-- +goose Down

drop table if exists mcp_servers;
