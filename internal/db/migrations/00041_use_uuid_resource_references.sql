-- +goose Up

alter table files
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table skills
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table skill_versions
	add column workspace_uuid uuid,
	add column skill_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table agents
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table agent_versions
	add column workspace_uuid uuid,
	add column agent_uuid uuid;
alter table environments
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table environment_keys
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column environment_uuid uuid;
alter table vaults
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table vault_credentials
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column vault_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table mcp_oauth_flows
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column vault_uuid uuid,
	add column user_uuid uuid;
alter table memory_stores
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table memories
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column memory_store_uuid uuid,
	add column current_version_uuid uuid;
alter table memory_versions
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column memory_store_uuid uuid,
	add column memory_uuid uuid,
	add column created_by_api_key_uuid uuid,
	add column redacted_by_api_key_uuid uuid;
alter table webhook_endpoints
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table message_batches
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid;
alter table message_batch_requests
	add column workspace_uuid uuid,
	add column message_batch_uuid uuid;

update files r
set workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from workspaces w, api_keys k
where w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update skills r
set workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from workspaces w, api_keys k
where w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update skill_versions r
set workspace_uuid = w.uuid,
	skill_uuid = p.uuid,
	created_by_api_key_uuid = k.uuid
from workspaces w, skills p, api_keys k
where w.id = r.workspace_id
	and p.id = r.skill_id
	and k.id = r.created_by_api_key_id;

update agents r
set workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from workspaces w, api_keys k
where w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update agent_versions r
set workspace_uuid = w.uuid,
	agent_uuid = p.uuid
from workspaces w, agents p
where w.id = r.workspace_id
	and p.id = r.agent_id;

update environments r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from organizations o, workspaces w, api_keys k
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update environment_keys r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	environment_uuid = p.uuid
from organizations o, workspaces w, environments p
where o.id = r.organization_id
	and w.id = r.workspace_id
	and p.id = r.environment_id;

update vaults r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from organizations o, workspaces w, api_keys k
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update vault_credentials r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	vault_uuid = p.uuid,
	created_by_api_key_uuid = (
		select k.uuid from api_keys k where k.id = r.created_by_api_key_id
	)
from organizations o, workspaces w, vaults p
where o.id = r.organization_id
	and w.id = r.workspace_id
	and p.id = r.vault_id;

update mcp_oauth_flows r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	vault_uuid = p.uuid,
	user_uuid = (
		select u.uuid from users u where u.id = r.user_id
	)
from organizations o, workspaces w, vaults p
where o.id = r.organization_id
	and w.id = r.workspace_id
	and p.id = r.vault_id;

update memory_stores r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from organizations o, workspaces w, api_keys k
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update memories r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	memory_store_uuid = p.uuid,
	current_version_uuid = (
		select v.uuid from memory_versions v where v.id = r.current_version_id
	)
from organizations o, workspaces w, memory_stores p
where o.id = r.organization_id
	and w.id = r.workspace_id
	and p.id = r.memory_store_id;

update memory_versions r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	memory_store_uuid = s.uuid,
	memory_uuid = m.uuid,
	created_by_api_key_uuid = (
		select k.uuid from api_keys k where k.id = r.created_by_api_key_id
	),
	redacted_by_api_key_uuid = (
		select k.uuid from api_keys k where k.id = r.redacted_by_api_key_id
	)
from organizations o, workspaces w, memory_stores s, memories m
where o.id = r.organization_id
	and w.id = r.workspace_id
	and s.id = r.memory_store_id
	and m.id = r.memory_id;

update webhook_endpoints r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from organizations o, workspaces w, api_keys k
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update message_batches r
set workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid
from workspaces w, api_keys k
where w.id = r.workspace_id
	and k.id = r.created_by_api_key_id;

update message_batch_requests r
set workspace_uuid = w.uuid,
	message_batch_uuid = p.uuid
from workspaces w, message_batches p
where w.id = r.workspace_id
	and p.id = r.message_batch_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from vault_credentials
		where created_by_api_key_id is not null and created_by_api_key_uuid is null
	) then
		raise exception 'vault_credentials contains an unmapped created_by_api_key_id';
	end if;
	if exists (
		select 1 from mcp_oauth_flows
		where user_id is not null and user_uuid is null
	) then
		raise exception 'mcp_oauth_flows contains an unmapped user_id';
	end if;
	if exists (
		select 1 from memories
		where current_version_id is not null and current_version_uuid is null
	) then
		raise exception 'memories contains an unmapped current_version_id';
	end if;
	if exists (
		select 1 from memory_versions
		where (created_by_api_key_id is not null and created_by_api_key_uuid is null)
			or (redacted_by_api_key_id is not null and redacted_by_api_key_uuid is null)
	) then
		raise exception 'memory_versions contains an unmapped API key reference';
	end if;
end $$;
-- +goose StatementEnd

alter table files
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table skills
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table skill_versions
	alter column workspace_uuid set not null,
	alter column skill_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table agents
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table agent_versions
	alter column workspace_uuid set not null,
	alter column agent_uuid set not null;
alter table environments
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table environment_keys
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column environment_uuid set not null;
alter table vaults
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table vault_credentials
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column vault_uuid set not null;
alter table mcp_oauth_flows
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column vault_uuid set not null;
alter table memory_stores
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table memories
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column memory_store_uuid set not null;
alter table memory_versions
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column memory_store_uuid set not null,
	alter column memory_uuid set not null;
alter table webhook_endpoints
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table message_batches
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null;
alter table message_batch_requests
	alter column workspace_uuid set not null,
	alter column message_batch_uuid set not null;

alter table files drop column workspace_id, drop column created_by_api_key_id;
alter table skills drop column workspace_id, drop column created_by_api_key_id;
alter table skill_versions drop column workspace_id, drop column skill_id, drop column created_by_api_key_id;
alter table agents drop column workspace_id, drop column created_by_api_key_id;
alter table agent_versions drop column workspace_id, drop column agent_id;
alter table environments drop column organization_id, drop column workspace_id, drop column created_by_api_key_id;
alter table environment_keys drop column organization_id, drop column workspace_id, drop column environment_id;
alter table vaults drop column organization_id, drop column workspace_id, drop column created_by_api_key_id;
alter table vault_credentials drop column organization_id, drop column workspace_id, drop column vault_id, drop column created_by_api_key_id;
alter table mcp_oauth_flows drop column organization_id, drop column workspace_id, drop column vault_id, drop column user_id;
alter table memory_stores drop column organization_id, drop column workspace_id, drop column created_by_api_key_id;
alter table memories drop column organization_id, drop column workspace_id, drop column memory_store_id, drop column current_version_id;
alter table memory_versions
	drop column organization_id,
	drop column workspace_id,
	drop column memory_store_id,
	drop column memory_id,
	drop column created_by_api_key_id,
	drop column redacted_by_api_key_id;
alter table webhook_endpoints drop column organization_id, drop column workspace_id, drop column created_by_api_key_id;
alter table message_batches drop column workspace_id, drop column created_by_api_key_id;
alter table message_batch_requests drop column workspace_id, drop column message_batch_id;

alter table skills
	add constraint skills_workspace_external_id_key unique (workspace_uuid, external_id);
alter table skill_versions
	add constraint skill_versions_skill_version_key unique (skill_uuid, version);
alter table agents
	add constraint agents_workspace_external_id_key unique (workspace_uuid, external_id);
alter table agent_versions
	add constraint agent_versions_agent_version_key unique (agent_uuid, version),
	add constraint agent_versions_workspace_agent_version_key unique (workspace_uuid, agent_external_id, version);
alter table environments
	add constraint environments_workspace_external_id_key unique (workspace_uuid, external_id);
alter table vaults
	add constraint vaults_workspace_external_id_key unique (workspace_uuid, external_id);
alter table vault_credentials
	add constraint vault_credentials_workspace_external_id_key unique (workspace_uuid, external_id);
alter table memory_stores
	add constraint memory_stores_workspace_external_id_key unique (workspace_uuid, external_id);
alter table memories
	add constraint memories_workspace_external_id_key unique (workspace_uuid, external_id);
alter table memory_versions
	add constraint memory_versions_workspace_external_id_key unique (workspace_uuid, external_id);
alter table webhook_endpoints
	add constraint webhook_endpoints_workspace_external_id_key unique (workspace_uuid, external_id);
alter table message_batch_requests
	add constraint message_batch_requests_custom_id_key unique (message_batch_uuid, custom_id),
	add constraint message_batch_requests_index_key unique (message_batch_uuid, request_index);

create index files_workspace_created_v3_idx
	on files (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index files_workspace_scope_v3_idx
	on files (workspace_uuid, scope_id)
	where deleted_at is null and scope_id is not null;
create index skills_workspace_created_v2_idx
	on skills (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create unique index skills_workspace_display_title_active_key
	on skills (workspace_uuid, display_title)
	where deleted_at is null;
create index skill_versions_skill_created_v2_idx
	on skill_versions (skill_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index skill_versions_workspace_skill_v2_idx
	on skill_versions (workspace_uuid, skill_uuid, version)
	where deleted_at is null;
create index agents_workspace_created_v2_idx
	on agents (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index agents_workspace_active_created_v2_idx
	on agents (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create index agent_versions_workspace_agent_version_v2_idx
	on agent_versions (workspace_uuid, agent_uuid, version desc, uuid desc);
create unique index environments_workspace_name_active_v2_key
	on environments (workspace_uuid, name)
	where deleted_at is null;
create index environments_workspace_created_v2_idx
	on environments (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index environments_workspace_active_created_v2_idx
	on environments (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create index environment_keys_environment_v2_idx
	on environment_keys (workspace_uuid, environment_uuid)
	where status = 'active';
create index vaults_workspace_created_v2_idx
	on vaults (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index vaults_workspace_active_created_v2_idx
	on vaults (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create unique index vault_credentials_vault_key_active_v2_key
	on vault_credentials (vault_uuid, credential_key)
	where deleted_at is null and archived_at is null;
create index vault_credentials_vault_created_v2_idx
	on vault_credentials (workspace_uuid, vault_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index vault_credentials_vault_active_created_v2_idx
	on vault_credentials (workspace_uuid, vault_uuid, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create index memory_stores_workspace_created_v2_idx
	on memory_stores (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index memory_stores_workspace_active_created_v2_idx
	on memory_stores (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create unique index memories_store_path_active_v2_key
	on memories (memory_store_uuid, path)
	where deleted_at is null;
create index memories_store_path_v2_idx
	on memories (workspace_uuid, memory_store_uuid, path asc, uuid asc)
	where deleted_at is null;
create index memories_store_created_v2_idx
	on memories (workspace_uuid, memory_store_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index memories_store_updated_v2_idx
	on memories (workspace_uuid, memory_store_uuid, updated_at desc, uuid desc)
	where deleted_at is null;
create index memory_versions_memory_created_v2_idx
	on memory_versions (workspace_uuid, memory_uuid, created_at desc, uuid desc);
create index memory_versions_store_created_v2_idx
	on memory_versions (workspace_uuid, memory_store_uuid, created_at desc, uuid desc);
create index memory_versions_store_operation_created_v2_idx
	on memory_versions (workspace_uuid, memory_store_uuid, operation, created_at desc, uuid desc);
create index memory_versions_store_api_key_created_v2_idx
	on memory_versions (workspace_uuid, memory_store_uuid, created_by_api_key_uuid, created_at desc, uuid desc)
	where created_by_api_key_uuid is not null;
create index memory_versions_store_session_created_v2_idx
	on memory_versions (workspace_uuid, memory_store_uuid, created_by_session_id, created_at desc, uuid desc)
	where created_by_session_id is not null;
create index webhook_endpoints_workspace_created_v2_idx
	on webhook_endpoints (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index webhook_endpoints_workspace_status_v2_idx
	on webhook_endpoints (workspace_uuid, status, created_at desc)
	where deleted_at is null;
create index message_batches_workspace_created_v2_idx
	on message_batches (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index message_batches_workspace_status_v2_idx
	on message_batches (workspace_uuid, processing_status, created_at desc)
	where deleted_at is null;
create index message_batch_requests_batch_index_v2_idx
	on message_batch_requests (message_batch_uuid, request_index);
create index message_batch_requests_batch_status_v2_idx
	on message_batch_requests (message_batch_uuid, status);
create index message_batch_requests_workspace_batch_v2_idx
	on message_batch_requests (workspace_uuid, message_batch_uuid);

-- +goose Down

alter table files add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table skills add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table skill_versions add column workspace_id bigint, add column skill_id bigint, add column created_by_api_key_id bigint;
alter table agents add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table agent_versions add column workspace_id bigint, add column agent_id bigint;
alter table environments add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table environment_keys add column organization_id bigint, add column workspace_id bigint, add column environment_id bigint;
alter table vaults add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table vault_credentials add column organization_id bigint, add column workspace_id bigint, add column vault_id bigint, add column created_by_api_key_id bigint;
alter table mcp_oauth_flows add column organization_id bigint, add column workspace_id bigint, add column vault_id bigint, add column user_id bigint;
alter table memory_stores add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table memories add column organization_id bigint, add column workspace_id bigint, add column memory_store_id bigint, add column current_version_id bigint;
alter table memory_versions
	add column organization_id bigint,
	add column workspace_id bigint,
	add column memory_store_id bigint,
	add column memory_id bigint,
	add column created_by_api_key_id bigint,
	add column redacted_by_api_key_id bigint;
alter table webhook_endpoints add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table message_batches add column workspace_id bigint, add column created_by_api_key_id bigint;
alter table message_batch_requests add column workspace_id bigint, add column message_batch_id bigint;

update files r set workspace_id = w.id, created_by_api_key_id = k.id
from workspaces w, api_keys k where w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update skills r set workspace_id = w.id, created_by_api_key_id = k.id
from workspaces w, api_keys k where w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update skill_versions r set workspace_id = w.id, skill_id = p.id, created_by_api_key_id = k.id
from workspaces w, skills p, api_keys k
where w.uuid = r.workspace_uuid and p.uuid = r.skill_uuid and k.uuid = r.created_by_api_key_uuid;
update agents r set workspace_id = w.id, created_by_api_key_id = k.id
from workspaces w, api_keys k where w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update agent_versions r set workspace_id = w.id, agent_id = p.id
from workspaces w, agents p where w.uuid = r.workspace_uuid and p.uuid = r.agent_uuid;
update environments r set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id
from organizations o, workspaces w, api_keys k
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update environment_keys r set organization_id = o.id, workspace_id = w.id, environment_id = p.id
from organizations o, workspaces w, environments p
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and p.uuid = r.environment_uuid;
update vaults r set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id
from organizations o, workspaces w, api_keys k
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update vault_credentials r
set organization_id = o.id,
	workspace_id = w.id,
	vault_id = p.id,
	created_by_api_key_id = (
		select k.id from api_keys k where k.uuid = r.created_by_api_key_uuid
	)
from organizations o, workspaces w, vaults p
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and p.uuid = r.vault_uuid;
update mcp_oauth_flows r
set organization_id = o.id,
	workspace_id = w.id,
	vault_id = p.id,
	user_id = (
		select u.id from users u where u.uuid = r.user_uuid
	)
from organizations o, workspaces w, vaults p
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and p.uuid = r.vault_uuid;
update memory_stores r set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id
from organizations o, workspaces w, api_keys k
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update memories r
set organization_id = o.id,
	workspace_id = w.id,
	memory_store_id = p.id,
	current_version_id = (
		select v.id from memory_versions v where v.uuid = r.current_version_uuid
	)
from organizations o, workspaces w, memory_stores p
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and p.uuid = r.memory_store_uuid;
update memory_versions r
set organization_id = o.id, workspace_id = w.id, memory_store_id = s.id, memory_id = m.id,
	created_by_api_key_id = (
		select k.id from api_keys k where k.uuid = r.created_by_api_key_uuid
	),
	redacted_by_api_key_id = (
		select k.id from api_keys k where k.uuid = r.redacted_by_api_key_uuid
	)
from organizations o, workspaces w, memory_stores s, memories m
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid
	and s.uuid = r.memory_store_uuid and m.uuid = r.memory_uuid;
update webhook_endpoints r set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id
from organizations o, workspaces w, api_keys k
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update message_batches r set workspace_id = w.id, created_by_api_key_id = k.id
from workspaces w, api_keys k where w.uuid = r.workspace_uuid and k.uuid = r.created_by_api_key_uuid;
update message_batch_requests r set workspace_id = w.id, message_batch_id = p.id
from workspaces w, message_batches p where w.uuid = r.workspace_uuid and p.uuid = r.message_batch_uuid;

alter table files alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table skills alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table skill_versions alter column workspace_id set not null, alter column skill_id set not null, alter column created_by_api_key_id set not null;
alter table agents alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table agent_versions alter column workspace_id set not null, alter column agent_id set not null;
alter table environments alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table environment_keys alter column organization_id set not null, alter column workspace_id set not null, alter column environment_id set not null;
alter table vaults alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table vault_credentials alter column organization_id set not null, alter column workspace_id set not null, alter column vault_id set not null, alter column created_by_api_key_id set not null;
alter table mcp_oauth_flows alter column organization_id set not null, alter column workspace_id set not null, alter column vault_id set not null;
alter table memory_stores alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table memories alter column organization_id set not null, alter column workspace_id set not null, alter column memory_store_id set not null;
alter table memory_versions alter column organization_id set not null, alter column workspace_id set not null, alter column memory_store_id set not null, alter column memory_id set not null;
alter table webhook_endpoints alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table message_batches alter column workspace_id set not null, alter column created_by_api_key_id set not null;
alter table message_batch_requests alter column workspace_id set not null, alter column message_batch_id set not null;

alter table files drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table skills drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table skill_versions drop column workspace_uuid, drop column skill_uuid, drop column created_by_api_key_uuid;
alter table agents drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table agent_versions drop column workspace_uuid, drop column agent_uuid;
alter table environments drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table environment_keys drop column organization_uuid, drop column workspace_uuid, drop column environment_uuid;
alter table vaults drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table vault_credentials drop column organization_uuid, drop column workspace_uuid, drop column vault_uuid, drop column created_by_api_key_uuid;
alter table mcp_oauth_flows drop column organization_uuid, drop column workspace_uuid, drop column vault_uuid, drop column user_uuid;
alter table memory_stores drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table memories drop column organization_uuid, drop column workspace_uuid, drop column memory_store_uuid, drop column current_version_uuid;
alter table memory_versions
	drop column organization_uuid,
	drop column workspace_uuid,
	drop column memory_store_uuid,
	drop column memory_uuid,
	drop column created_by_api_key_uuid,
	drop column redacted_by_api_key_uuid;
alter table webhook_endpoints drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table message_batches drop column workspace_uuid, drop column created_by_api_key_uuid;
alter table message_batch_requests drop column workspace_uuid, drop column message_batch_uuid;

alter table skills add constraint skills_workspace_external_id_key unique (workspace_id, external_id);
alter table skill_versions add constraint skill_versions_skill_version_key unique (skill_id, version);
alter table agents add constraint agents_workspace_external_id_key unique (workspace_id, external_id);
alter table agent_versions
	add constraint agent_versions_agent_version_key unique (agent_id, version),
	add constraint agent_versions_workspace_agent_version_key unique (workspace_id, agent_external_id, version);
alter table environments add constraint environments_workspace_external_id_key unique (workspace_id, external_id);
alter table vaults add constraint vaults_workspace_external_id_key unique (workspace_id, external_id);
alter table vault_credentials add constraint vault_credentials_workspace_external_id_key unique (workspace_id, external_id);
alter table memory_stores add constraint memory_stores_workspace_external_id_key unique (workspace_id, external_id);
alter table memories add constraint memories_workspace_external_id_key unique (workspace_id, external_id);
alter table memory_versions add constraint memory_versions_workspace_external_id_key unique (workspace_id, external_id);
alter table webhook_endpoints add constraint webhook_endpoints_workspace_external_id_key unique (workspace_id, external_id);
alter table message_batch_requests
	add constraint message_batch_requests_custom_id_key unique (message_batch_id, custom_id),
	add constraint message_batch_requests_index_key unique (message_batch_id, request_index);

create index files_workspace_created_v2_idx on files (workspace_id, created_at desc, id desc) where deleted_at is null;
create index files_workspace_scope_v2_idx on files (workspace_id, scope_id) where deleted_at is null and scope_id is not null;
create index skills_workspace_created_v1_idx on skills (workspace_id, created_at desc, id desc) where deleted_at is null;
create unique index skills_workspace_display_title_active_key on skills (workspace_id, display_title) where deleted_at is null;
create index skill_versions_skill_created_v1_idx on skill_versions (skill_id, created_at desc, id desc) where deleted_at is null;
create index skill_versions_workspace_skill_v1_idx on skill_versions (workspace_id, skill_external_id, version) where deleted_at is null;
create index agents_workspace_created_v1_idx on agents (workspace_id, created_at desc, id desc) where deleted_at is null;
create index agents_workspace_active_created_v1_idx on agents (workspace_id, created_at desc, id desc) where deleted_at is null and archived_at is null;
create index agent_versions_workspace_agent_version_v1_idx on agent_versions (workspace_id, agent_external_id, version desc, id desc);
create unique index environments_workspace_name_active_v1_key on environments (workspace_id, name) where deleted_at is null;
create index environments_workspace_created_v1_idx on environments (workspace_id, created_at desc, id desc) where deleted_at is null;
create index environments_workspace_active_created_v1_idx on environments (workspace_id, created_at desc, id desc) where deleted_at is null and archived_at is null;
create index environment_keys_environment_v1_idx on environment_keys (workspace_id, environment_external_id) where status = 'active';
create index vaults_workspace_created_v1_idx on vaults (workspace_id, created_at desc, id desc) where deleted_at is null;
create index vaults_workspace_active_created_v1_idx on vaults (workspace_id, created_at desc, id desc) where deleted_at is null and archived_at is null;
create unique index vault_credentials_vault_key_active_v1_key on vault_credentials (vault_id, credential_key) where deleted_at is null and archived_at is null;
create index vault_credentials_vault_created_v1_idx on vault_credentials (workspace_id, vault_external_id, created_at desc, id desc) where deleted_at is null;
create index vault_credentials_vault_active_created_v1_idx on vault_credentials (workspace_id, vault_external_id, created_at desc, id desc) where deleted_at is null and archived_at is null;
create index memory_stores_workspace_created_v1_idx on memory_stores (workspace_id, created_at desc, id desc) where deleted_at is null;
create index memory_stores_workspace_active_created_v1_idx on memory_stores (workspace_id, created_at desc, id desc) where deleted_at is null and archived_at is null;
create unique index memories_store_path_active_v1_key on memories (memory_store_id, path) where deleted_at is null;
create index memories_store_path_v1_idx on memories (workspace_id, memory_store_external_id, path asc, id asc) where deleted_at is null;
create index memories_store_created_v1_idx on memories (workspace_id, memory_store_external_id, created_at desc, id desc) where deleted_at is null;
create index memories_store_updated_v1_idx on memories (workspace_id, memory_store_external_id, updated_at desc, id desc) where deleted_at is null;
create index memory_versions_store_created_v1_idx on memory_versions (workspace_id, memory_store_external_id, created_at desc, id desc);
create index memory_versions_memory_created_v1_idx on memory_versions (workspace_id, memory_external_id, created_at desc, id desc);
create index memory_versions_store_operation_created_v1_idx on memory_versions (workspace_id, memory_store_external_id, operation, created_at desc, id desc);
create index memory_versions_store_api_key_created_v1_idx on memory_versions (workspace_id, memory_store_external_id, created_by_api_key_external_id, created_at desc, id desc) where created_by_api_key_external_id is not null;
create index memory_versions_store_session_created_v1_idx on memory_versions (workspace_id, memory_store_external_id, created_by_session_id, created_at desc, id desc) where created_by_session_id is not null;
create index webhook_endpoints_workspace_created_v1_idx on webhook_endpoints (workspace_id, created_at desc, id desc) where deleted_at is null;
create index webhook_endpoints_workspace_status_v1_idx on webhook_endpoints (workspace_id, status, created_at desc) where deleted_at is null;
create index message_batches_workspace_created_v1_idx on message_batches (workspace_id, created_at desc, id desc) where deleted_at is null;
create index message_batches_workspace_status_v1_idx on message_batches (workspace_id, processing_status, created_at desc) where deleted_at is null;
create index message_batch_requests_batch_index_v1_idx on message_batch_requests (message_batch_id, request_index);
create index message_batch_requests_batch_status_v1_idx on message_batch_requests (message_batch_id, status);
create index message_batch_requests_workspace_batch_v1_idx on message_batch_requests (workspace_id, message_batch_id);
