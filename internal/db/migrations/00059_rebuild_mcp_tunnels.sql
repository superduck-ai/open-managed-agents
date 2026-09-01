-- +goose Up

-- The legacy tunnel resource was a pre-release placeholder. The old Tunnel
-- schema reached its current shape at version 45 and remains unchanged through
-- the shared version 58 baseline, so version 59 deliberately discards every legacy tunnel,
-- connector token, and certificate row instead of migrating plaintext tokens,
-- nullable workspace ownership, or obsolete IDs into the public contract.
-- The certificate table itself is retained for the existing API boundary.

drop table if exists mcp_tunnel_token_versions;
drop table if exists mcp_tunnels_rebuilt;
delete from mcp_tunnel_certificates;

create table mcp_tunnels_rebuilt (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    organization_uuid uuid not null,
    workspace_uuid uuid not null,
    display_name text,
    domain text not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    archived_at timestamptz,
    constraint mcp_tunnels_rebuilt_id_pk primary key (id),
    constraint mcp_tunnels_rebuilt_uuid_key unique (uuid),
    constraint mcp_tunnels_rebuilt_external_id_key unique (external_id),
    constraint mcp_tunnels_rebuilt_external_id_format_check
        check (external_id ~ '^tunnel_[0-9a-f]{32}$'),
    constraint mcp_tunnels_rebuilt_domain_key unique (domain)
);

drop table mcp_tunnels;
alter table mcp_tunnels_rebuilt rename to mcp_tunnels;

alter table mcp_tunnels
    rename constraint mcp_tunnels_rebuilt_id_pk to mcp_tunnels_id_pk;
alter table mcp_tunnels
    rename constraint mcp_tunnels_rebuilt_uuid_key to mcp_tunnels_uuid_key;
alter table mcp_tunnels
    rename constraint mcp_tunnels_rebuilt_external_id_key to mcp_tunnels_external_id_key;
alter table mcp_tunnels
    rename constraint mcp_tunnels_rebuilt_external_id_format_check to mcp_tunnels_external_id_format_check;
alter table mcp_tunnels
    rename constraint mcp_tunnels_rebuilt_domain_key to mcp_tunnels_domain_key;

create index mcp_tunnels_workspace_created_v1_idx
    on mcp_tunnels (organization_uuid, workspace_uuid, created_at desc, uuid desc);

create table mcp_tunnel_token_versions (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    tunnel_uuid uuid not null,
    version bigint not null,
    token_hash bytea not null,
    ciphertext bytea,
    nonce bytea,
    wrapped_dek bytea,
    format_version int,
    key_provider text,
    key_version bigint,
    created_at timestamptz not null default now(),
    retired_at timestamptz,
    archived_at timestamptz,
    constraint mcp_tunnel_token_versions_id_pk primary key (id),
    constraint mcp_tunnel_token_versions_uuid_key unique (uuid),
    constraint mcp_tunnel_token_versions_external_id_key unique (external_id),
    constraint mcp_tunnel_token_versions_token_hash_key unique (token_hash),
    constraint mcp_tunnel_token_versions_tunnel_version_key unique (tunnel_uuid, version)
);

create unique index mcp_tunnel_token_versions_one_active_v1_idx
    on mcp_tunnel_token_versions (tunnel_uuid)
    where retired_at is null and archived_at is null;

create index mcp_tunnel_token_versions_tunnel_created_v1_idx
    on mcp_tunnel_token_versions (tunnel_uuid, created_at desc, uuid desc);

-- +goose Down

-- Rolling back returns the migration series to version 58 and restores the
-- pre-version-59 Tunnel schema. Data discarded by the Up migration
-- cannot be reconstructed.

drop table if exists mcp_tunnel_token_versions;
drop table if exists mcp_tunnels;

create table mcp_tunnels (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    organization_uuid uuid not null,
    workspace_uuid uuid,
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

create index mcp_tunnels_organization_created_v1_idx
    on mcp_tunnels (organization_uuid, created_at desc, uuid desc);
