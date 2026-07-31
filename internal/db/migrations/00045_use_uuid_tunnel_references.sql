-- +goose Up

alter table mcp_tunnels
	add column organization_uuid uuid,
	add column workspace_uuid uuid;

alter table mcp_tunnel_certificates
	add column organization_uuid uuid,
	add column tunnel_uuid uuid;

update mcp_tunnels t
set organization_uuid = o.uuid
from organizations o
where o.id = t.organization_id;

update mcp_tunnels t
set workspace_uuid = w.uuid
from workspaces w
where w.id = t.workspace_id;

update mcp_tunnel_certificates c
set organization_uuid = o.uuid
from organizations o
where o.id = c.organization_id;

update mcp_tunnel_certificates c
set tunnel_uuid = t.uuid
from mcp_tunnels t
where t.id = c.tunnel_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from mcp_tunnels t
		where t.organization_uuid is null
			or (t.workspace_id is not null and t.workspace_uuid is null)
	) or exists (
		select 1
		from mcp_tunnels t
		join workspaces w on w.uuid = t.workspace_uuid
		where w.organization_uuid <> t.organization_uuid
	) then
		raise exception 'cannot migrate MCP tunnel references to UUID';
	end if;

	if exists (
		select 1
		from mcp_tunnel_certificates c
		where c.organization_uuid is null
			or c.tunnel_uuid is null
	) or exists (
		select 1
		from mcp_tunnel_certificates c
		join mcp_tunnels t on t.uuid = c.tunnel_uuid
		where t.organization_uuid <> c.organization_uuid
	) then
		raise exception 'cannot migrate MCP tunnel certificate references to UUID';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists mcp_tunnels_organization_created_v1_idx;
drop index if exists mcp_tunnel_certificates_tunnel_created_v1_idx;

alter table mcp_tunnels
	alter column organization_uuid set not null,
	drop column organization_id,
	drop column workspace_id;

alter table mcp_tunnel_certificates
	alter column organization_uuid set not null,
	alter column tunnel_uuid set not null,
	drop column organization_id,
	drop column tunnel_id;

create index mcp_tunnels_organization_created_v1_idx
	on mcp_tunnels (organization_uuid, created_at desc, uuid desc);

create index mcp_tunnel_certificates_tunnel_created_v1_idx
	on mcp_tunnel_certificates (tunnel_uuid, created_at desc, uuid desc);

-- +goose Down

alter table mcp_tunnels
	add column organization_id bigint,
	add column workspace_id bigint;

alter table mcp_tunnel_certificates
	add column organization_id bigint,
	add column tunnel_id bigint;

update mcp_tunnels t
set organization_id = o.id
from organizations o
where o.uuid = t.organization_uuid;

update mcp_tunnels t
set workspace_id = w.id
from workspaces w
where w.uuid = t.workspace_uuid;

update mcp_tunnel_certificates c
set organization_id = o.id
from organizations o
where o.uuid = c.organization_uuid;

update mcp_tunnel_certificates c
set tunnel_id = t.id
from mcp_tunnels t
where t.uuid = c.tunnel_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from mcp_tunnels
		where organization_id is null
			or (workspace_uuid is not null and workspace_id is null)
	) or exists (
		select 1
		from mcp_tunnel_certificates
		where organization_id is null
			or tunnel_id is null
	) then
		raise exception 'cannot restore MCP tunnel bigint references';
	end if;
end $$;
-- +goose StatementEnd

drop index if exists mcp_tunnels_organization_created_v1_idx;
drop index if exists mcp_tunnel_certificates_tunnel_created_v1_idx;

alter table mcp_tunnels
	alter column organization_id set not null,
	drop column organization_uuid,
	drop column workspace_uuid;

alter table mcp_tunnel_certificates
	alter column organization_id set not null,
	alter column tunnel_id set not null,
	drop column organization_uuid,
	drop column tunnel_uuid;

create index mcp_tunnels_organization_created_v1_idx
	on mcp_tunnels (organization_id, created_at desc, id desc);

create index mcp_tunnel_certificates_tunnel_created_v1_idx
	on mcp_tunnel_certificates (tunnel_id, created_at desc, id desc);
