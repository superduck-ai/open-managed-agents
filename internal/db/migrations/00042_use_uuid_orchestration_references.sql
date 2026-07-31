-- +goose Up

alter table deployments
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid,
	add column environment_uuid uuid,
	add column agent_uuid uuid;
alter table deployment_runs
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid,
	add column deployment_uuid uuid,
	add column agent_uuid uuid;
alter table sessions
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column created_by_api_key_uuid uuid,
	add column environment_uuid uuid,
	add column agent_uuid uuid,
	add column deployment_uuid uuid;
alter table session_threads
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column session_uuid uuid,
	add column parent_thread_uuid uuid;
alter table session_resources
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column session_uuid uuid;
alter table session_events
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column session_uuid uuid,
	add column thread_uuid uuid;

update deployments r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid,
	environment_uuid = e.uuid,
	agent_uuid = a.uuid
from organizations o, workspaces w, api_keys k, environments e, agents a
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id
	and e.id = r.environment_id
	and a.id = r.agent_id;

update deployment_runs r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid,
	deployment_uuid = d.uuid,
	agent_uuid = a.uuid
from organizations o, workspaces w, api_keys k, deployments d, agents a
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id
	and d.id = r.deployment_id
	and a.id = r.agent_id;

update sessions r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	created_by_api_key_uuid = k.uuid,
	environment_uuid = e.uuid,
	agent_uuid = a.uuid,
	deployment_uuid = (
		select d.uuid
		from deployments d
		where d.external_id = r.deployment_id
			and d.workspace_uuid = w.uuid
	)
from organizations o, workspaces w, api_keys k, environments e, agents a
where o.id = r.organization_id
	and w.id = r.workspace_id
	and k.id = r.created_by_api_key_id
	and e.id = r.environment_id
	and a.id = r.agent_id;

update session_threads r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	session_uuid = s.uuid,
	parent_thread_uuid = (
		select p.uuid from session_threads p where p.id = r.parent_thread_id
	)
from organizations o, workspaces w, sessions s
where o.id = r.organization_id
	and w.id = r.workspace_id
	and s.id = r.session_id;

update session_resources r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	session_uuid = s.uuid
from organizations o, workspaces w, sessions s
where o.id = r.organization_id
	and w.id = r.workspace_id
	and s.id = r.session_id;

update session_events r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	session_uuid = s.uuid,
	thread_uuid = (
		select t.uuid from session_threads t where t.id = r.thread_id
	)
from organizations o, workspaces w, sessions s
where o.id = r.organization_id
	and w.id = r.workspace_id
	and s.id = r.session_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from sessions
		where deployment_id is not null and deployment_uuid is null
	) then
		raise exception 'sessions contains an unmapped deployment_id';
	end if;
	if exists (
		select 1 from session_threads
		where parent_thread_id is not null and parent_thread_uuid is null
	) then
		raise exception 'session_threads contains an unmapped parent_thread_id';
	end if;
	if exists (
		select 1 from session_events
		where thread_id is not null and thread_uuid is null
	) then
		raise exception 'session_events contains an unmapped thread_id';
	end if;
end $$;
-- +goose StatementEnd

alter table deployments
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null,
	alter column environment_uuid set not null,
	alter column agent_uuid set not null;
alter table deployment_runs
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null,
	alter column deployment_uuid set not null,
	alter column agent_uuid set not null;
alter table sessions
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column created_by_api_key_uuid set not null,
	alter column environment_uuid set not null,
	alter column agent_uuid set not null;
alter table session_threads
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column session_uuid set not null;
alter table session_resources
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column session_uuid set not null;
alter table session_events
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column session_uuid set not null;

alter table deployments
	drop column organization_id,
	drop column workspace_id,
	drop column created_by_api_key_id,
	drop column environment_id,
	drop column agent_id;
alter table deployment_runs
	drop column organization_id,
	drop column workspace_id,
	drop column created_by_api_key_id,
	drop column deployment_id,
	drop column agent_id;
alter table sessions
	drop column organization_id,
	drop column workspace_id,
	drop column created_by_api_key_id,
	drop column environment_id,
	drop column agent_id;
alter table sessions rename column deployment_id to deployment_external_id;
alter table session_threads
	drop column organization_id,
	drop column workspace_id,
	drop column session_id,
	drop column parent_thread_id;
alter table session_resources
	drop column organization_id,
	drop column workspace_id,
	drop column session_id;
alter table session_events
	drop column organization_id,
	drop column workspace_id,
	drop column session_id,
	drop column thread_id;

alter table deployments
	add constraint deployments_workspace_external_id_key unique (workspace_uuid, external_id);
alter table deployment_runs
	add constraint deployment_runs_workspace_external_id_key unique (workspace_uuid, external_id);
alter table sessions
	add constraint sessions_workspace_external_id_key unique (workspace_uuid, external_id);
alter table session_threads
	add constraint session_threads_workspace_external_id_key unique (workspace_uuid, external_id);
alter table session_resources
	add constraint session_resources_workspace_external_id_key unique (workspace_uuid, external_id);
alter table session_events
	add constraint session_events_workspace_external_id_key unique (workspace_uuid, external_id);

create index deployments_workspace_created_v2_idx
	on deployments (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index deployments_workspace_active_created_v2_idx
	on deployments (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create index deployments_workspace_status_created_v2_idx
	on deployments (workspace_uuid, status, created_at desc, uuid desc)
	where deleted_at is null and archived_at is null;
create index deployments_workspace_agent_created_v2_idx
	on deployments (workspace_uuid, agent_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index deployment_runs_workspace_created_v2_idx
	on deployment_runs (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index deployment_runs_workspace_deployment_created_v2_idx
	on deployment_runs (workspace_uuid, deployment_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index deployment_runs_workspace_trigger_created_v2_idx
	on deployment_runs (workspace_uuid, trigger_type, created_at desc, uuid desc)
	where deleted_at is null;
create index deployment_runs_workspace_error_created_v2_idx
	on deployment_runs (workspace_uuid, (error is not null), created_at desc, uuid desc)
	where deleted_at is null;
create index sessions_workspace_created_v2_idx
	on sessions (workspace_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index sessions_workspace_status_created_v2_idx
	on sessions (workspace_uuid, status, created_at desc, uuid desc)
	where deleted_at is null;
create index sessions_workspace_agent_created_v2_idx
	on sessions (workspace_uuid, agent_uuid, agent_version, created_at desc, uuid desc)
	where deleted_at is null;
create index sessions_workspace_deployment_created_v2_idx
	on sessions (workspace_uuid, deployment_uuid, created_at desc, uuid desc)
	where deleted_at is null and deployment_uuid is not null;
create index session_threads_session_created_v2_idx
	on session_threads (workspace_uuid, session_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index session_resources_session_created_v2_idx
	on session_resources (workspace_uuid, session_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index session_resources_memory_store_v2_idx
	on session_resources (workspace_uuid, (payload->>'memory_store_id'))
	where deleted_at is null and resource_type = 'memory_store';
create index session_events_session_created_v2_idx
	on session_events (workspace_uuid, session_uuid, created_at asc, uuid asc)
	where deleted_at is null;
create index session_events_thread_created_v2_idx
	on session_events (workspace_uuid, session_uuid, thread_uuid, created_at asc, uuid asc)
	where deleted_at is null and thread_uuid is not null;
create index session_events_type_created_v2_idx
	on session_events (workspace_uuid, event_type, created_at asc, uuid asc)
	where deleted_at is null;

-- +goose Down

alter table deployments add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint, add column environment_id bigint, add column agent_id bigint;
alter table deployment_runs add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint, add column deployment_id bigint, add column agent_id bigint;
alter table sessions rename column deployment_external_id to deployment_id;
alter table sessions add column organization_id bigint, add column workspace_id bigint, add column created_by_api_key_id bigint, add column environment_id bigint, add column agent_id bigint;
alter table session_threads add column organization_id bigint, add column workspace_id bigint, add column session_id bigint, add column parent_thread_id bigint;
alter table session_resources add column organization_id bigint, add column workspace_id bigint, add column session_id bigint;
alter table session_events add column organization_id bigint, add column workspace_id bigint, add column session_id bigint, add column thread_id bigint;

update deployments r
set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id, environment_id = e.id, agent_id = a.id
from organizations o, workspaces w, api_keys k, environments e, agents a
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid
	and k.uuid = r.created_by_api_key_uuid and e.uuid = r.environment_uuid and a.uuid = r.agent_uuid;
update deployment_runs r
set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id, deployment_id = d.id, agent_id = a.id
from organizations o, workspaces w, api_keys k, deployments d, agents a
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid
	and k.uuid = r.created_by_api_key_uuid and d.uuid = r.deployment_uuid and a.uuid = r.agent_uuid;
update sessions r
set organization_id = o.id, workspace_id = w.id, created_by_api_key_id = k.id,
	environment_id = e.id, agent_id = a.id,
	deployment_id = (
		select d.external_id from deployments d where d.uuid = r.deployment_uuid
	)
from organizations o, workspaces w, api_keys k, environments e, agents a
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid
	and k.uuid = r.created_by_api_key_uuid and e.uuid = r.environment_uuid and a.uuid = r.agent_uuid;
update session_threads r
set organization_id = o.id, workspace_id = w.id, session_id = s.id,
	parent_thread_id = (
		select p.id from session_threads p where p.uuid = r.parent_thread_uuid
	)
from organizations o, workspaces w, sessions s
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and s.uuid = r.session_uuid;
update session_resources r
set organization_id = o.id, workspace_id = w.id, session_id = s.id
from organizations o, workspaces w, sessions s
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and s.uuid = r.session_uuid;
update session_events r
set organization_id = o.id, workspace_id = w.id, session_id = s.id,
	thread_id = (
		select t.id from session_threads t where t.uuid = r.thread_uuid
	)
from organizations o, workspaces w, sessions s
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and s.uuid = r.session_uuid;

alter table deployments alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null, alter column environment_id set not null, alter column agent_id set not null;
alter table deployment_runs alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null, alter column deployment_id set not null, alter column agent_id set not null;
alter table sessions alter column organization_id set not null, alter column workspace_id set not null, alter column created_by_api_key_id set not null, alter column environment_id set not null, alter column agent_id set not null;
alter table session_threads alter column organization_id set not null, alter column workspace_id set not null, alter column session_id set not null;
alter table session_resources alter column organization_id set not null, alter column workspace_id set not null, alter column session_id set not null;
alter table session_events alter column organization_id set not null, alter column workspace_id set not null, alter column session_id set not null;

alter table deployments drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid, drop column environment_uuid, drop column agent_uuid;
alter table deployment_runs drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid, drop column deployment_uuid, drop column agent_uuid;
alter table sessions drop column organization_uuid, drop column workspace_uuid, drop column created_by_api_key_uuid, drop column environment_uuid, drop column agent_uuid, drop column deployment_uuid;
alter table session_threads drop column organization_uuid, drop column workspace_uuid, drop column session_uuid, drop column parent_thread_uuid;
alter table session_resources drop column organization_uuid, drop column workspace_uuid, drop column session_uuid;
alter table session_events drop column organization_uuid, drop column workspace_uuid, drop column session_uuid, drop column thread_uuid;

alter table deployments add constraint deployments_workspace_external_id_key unique (workspace_id, external_id);
alter table deployment_runs add constraint deployment_runs_workspace_external_id_key unique (workspace_id, external_id);
alter table sessions add constraint sessions_workspace_external_id_key unique (workspace_id, external_id);
alter table session_threads add constraint session_threads_workspace_external_id_key unique (workspace_id, external_id);
alter table session_resources add constraint session_resources_workspace_external_id_key unique (workspace_id, external_id);
alter table session_events add constraint session_events_workspace_external_id_key unique (workspace_id, external_id);

create index deployments_workspace_created_v1_idx on deployments (workspace_id, created_at desc, id desc) where deleted_at is null;
create index deployments_workspace_active_created_v1_idx on deployments (workspace_id, created_at desc, id desc) where deleted_at is null and archived_at is null;
create index deployments_workspace_status_created_v1_idx on deployments (workspace_id, status, created_at desc, id desc) where deleted_at is null and archived_at is null;
create index deployments_workspace_agent_created_v1_idx on deployments (workspace_id, agent_external_id, created_at desc, id desc) where deleted_at is null;
create index deployment_runs_workspace_created_v1_idx on deployment_runs (workspace_id, created_at desc, id desc) where deleted_at is null;
create index deployment_runs_workspace_deployment_created_v1_idx on deployment_runs (workspace_id, deployment_external_id, created_at desc, id desc) where deleted_at is null;
create index deployment_runs_workspace_trigger_created_v1_idx on deployment_runs (workspace_id, trigger_type, created_at desc, id desc) where deleted_at is null;
create index deployment_runs_workspace_error_created_v1_idx on deployment_runs (workspace_id, (error is not null), created_at desc, id desc) where deleted_at is null;
create index sessions_workspace_created_v1_idx on sessions (workspace_id, created_at desc, id desc) where deleted_at is null;
create index sessions_workspace_status_created_v1_idx on sessions (workspace_id, status, created_at desc, id desc) where deleted_at is null;
create index sessions_workspace_agent_created_v1_idx on sessions (workspace_id, agent_external_id, agent_version, created_at desc, id desc) where deleted_at is null;
create index sessions_workspace_deployment_created_v1_idx on sessions (workspace_id, deployment_id, created_at desc, id desc) where deleted_at is null and deployment_id is not null;
create index session_threads_session_created_v1_idx on session_threads (workspace_id, session_external_id, created_at desc, id desc) where deleted_at is null;
create index session_resources_session_created_v1_idx on session_resources (workspace_id, session_external_id, created_at desc, id desc) where deleted_at is null;
create index session_resources_memory_store_v1_idx on session_resources (workspace_id, (payload->>'memory_store_id')) where deleted_at is null and resource_type = 'memory_store';
create index session_events_session_created_v1_idx on session_events (workspace_id, session_external_id, created_at asc, id asc) where deleted_at is null;
create index session_events_thread_created_v1_idx on session_events (workspace_id, session_external_id, thread_external_id, created_at asc, id asc) where deleted_at is null and thread_external_id is not null;
create index session_events_type_created_v1_idx on session_events (workspace_id, event_type, created_at asc, id asc) where deleted_at is null;
