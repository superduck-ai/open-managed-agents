-- +goose Up

alter table code_sessions
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column session_uuid uuid,
	add column environment_uuid uuid;
alter table code_session_inbound_events
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column code_session_uuid uuid;
alter table code_session_outbound_events
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column code_session_uuid uuid;
alter table code_session_internal_events
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column code_session_uuid uuid;
alter table environment_work
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column environment_uuid uuid;
alter table environment_worker_polls
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column environment_uuid uuid;
alter table environment_sandboxes
	add column organization_uuid uuid,
	add column workspace_uuid uuid,
	add column environment_uuid uuid,
	add column work_uuid uuid;
alter table jobs add column workspace_uuid uuid;
alter table workspace_storage_usage add column workspace_uuid uuid;

update code_sessions r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	session_uuid = s.uuid,
	environment_uuid = e.uuid
from organizations o, workspaces w, sessions s, environments e
where o.id = r.organization_id
	and w.id = r.workspace_id
	and s.id = r.session_id
	and e.id = r.environment_id;

update code_session_inbound_events r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	code_session_uuid = c.uuid
from organizations o, workspaces w, code_sessions c
where o.id = r.organization_id
	and w.id = r.workspace_id
	and c.id = r.code_session_id;

update code_session_outbound_events r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	code_session_uuid = c.uuid
from organizations o, workspaces w, code_sessions c
where o.id = r.organization_id
	and w.id = r.workspace_id
	and c.id = r.code_session_id;

update code_session_internal_events r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	code_session_uuid = c.uuid
from organizations o, workspaces w, code_sessions c
where o.id = r.organization_id
	and w.id = r.workspace_id
	and c.id = r.code_session_id;

update environment_work r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	environment_uuid = e.uuid
from organizations o, workspaces w, environments e
where o.id = r.organization_id
	and w.id = r.workspace_id
	and e.id = r.environment_id;

update environment_worker_polls r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	environment_uuid = e.uuid
from organizations o, workspaces w, environments e
where o.id = r.organization_id
	and w.id = r.workspace_id
	and e.id = r.environment_id;

update environment_sandboxes r
set organization_uuid = o.uuid,
	workspace_uuid = w.uuid,
	environment_uuid = e.uuid,
	work_uuid = (
		select q.uuid from environment_work q where q.id = r.work_id
	)
from organizations o, workspaces w, environments e
where o.id = r.organization_id
	and w.id = r.workspace_id
	and e.id = r.environment_id;

update jobs r
set workspace_uuid = w.uuid
from workspaces w
where w.id = r.workspace_id;

update workspace_storage_usage r
set workspace_uuid = w.uuid
from workspaces w
where w.id = r.workspace_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1 from environment_sandboxes
		where work_id is not null and work_uuid is null
	) then
		raise exception 'environment_sandboxes contains an unmapped work_id';
	end if;
end $$;
-- +goose StatementEnd

alter table code_sessions
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column session_uuid set not null,
	alter column environment_uuid set not null;
alter table code_session_inbound_events
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column code_session_uuid set not null;
alter table code_session_outbound_events
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column code_session_uuid set not null;
alter table code_session_internal_events
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column code_session_uuid set not null;
alter table environment_work
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column environment_uuid set not null;
alter table environment_worker_polls
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column environment_uuid set not null;
alter table environment_sandboxes
	alter column organization_uuid set not null,
	alter column workspace_uuid set not null,
	alter column environment_uuid set not null;
alter table jobs alter column workspace_uuid set not null;
alter table workspace_storage_usage alter column workspace_uuid set not null;

alter table code_sessions
	drop column organization_id,
	drop column workspace_id,
	drop column session_id,
	drop column environment_id;
alter table code_session_inbound_events
	drop column organization_id,
	drop column workspace_id,
	drop column code_session_id;
alter table code_session_outbound_events
	drop column organization_id,
	drop column workspace_id,
	drop column code_session_id;
alter table code_session_internal_events
	drop column organization_id,
	drop column workspace_id,
	drop column code_session_id;
alter table environment_work
	drop column organization_id,
	drop column workspace_id,
	drop column environment_id;
alter table environment_worker_polls
	drop column organization_id,
	drop column workspace_id,
	drop column environment_id;
alter table environment_sandboxes
	drop column organization_id,
	drop column workspace_id,
	drop column environment_id,
	drop column work_id;
alter table jobs drop column workspace_id;
alter table workspace_storage_usage drop column workspace_id;

alter table code_sessions
	add constraint code_sessions_workspace_external_id_key unique (workspace_uuid, external_id);
alter table code_session_inbound_events
	add constraint code_session_inbound_events_workspace_external_id_key unique (workspace_uuid, external_id);
alter table code_session_outbound_events
	add constraint code_session_outbound_events_workspace_external_id_key unique (workspace_uuid, external_id);
alter table code_session_internal_events
	add constraint code_session_internal_events_workspace_external_id_key unique (workspace_uuid, external_id);
alter table environment_work
	add constraint environment_work_workspace_external_id_key unique (workspace_uuid, external_id);
alter table environment_worker_polls
	add constraint environment_worker_polls_environment_worker_key unique (environment_uuid, worker_id);
alter table workspace_storage_usage
	add constraint workspace_storage_usage_pk primary key (workspace_uuid);

create index code_sessions_public_session_v2_idx
	on code_sessions (workspace_uuid, session_uuid)
	where deleted_at is null;
create unique index code_session_inbound_events_sequence_v2_key
	on code_session_inbound_events (workspace_uuid, code_session_uuid, sequence_num)
	where deleted_at is null;
create unique index code_session_inbound_events_idempotency_v2_key
	on code_session_inbound_events (workspace_uuid, idempotency_key)
	where deleted_at is null and idempotency_key <> '';
create index code_session_inbound_events_queued_v2_idx
	on code_session_inbound_events (workspace_uuid, code_session_uuid, sequence_num asc)
	where deleted_at is null and delivery_status = 'queued';
create unique index code_session_outbound_events_sequence_v2_key
	on code_session_outbound_events (workspace_uuid, code_session_uuid, sequence_num)
	where deleted_at is null;
create unique index code_session_outbound_events_idempotency_v2_key
	on code_session_outbound_events (workspace_uuid, idempotency_key)
	where deleted_at is null and idempotency_key <> '';
create index code_session_outbound_events_created_v2_idx
	on code_session_outbound_events (workspace_uuid, code_session_uuid, created_at asc, uuid asc)
	where deleted_at is null;
create unique index code_session_internal_events_sequence_v2_key
	on code_session_internal_events (workspace_uuid, code_session_uuid, sequence_num)
	where deleted_at is null;
create unique index code_session_internal_events_idempotency_v2_key
	on code_session_internal_events (workspace_uuid, idempotency_key)
	where deleted_at is null and idempotency_key <> '';
create index code_session_internal_events_foreground_list_v2_idx
	on code_session_internal_events (workspace_uuid, code_session_uuid, sequence_num asc)
	where deleted_at is null and agent_id is null;
create index code_session_internal_events_subagent_list_v2_idx
	on code_session_internal_events (workspace_uuid, code_session_uuid, agent_id, sequence_num asc)
	where deleted_at is null and agent_id is not null;
create index code_session_internal_events_foreground_compaction_v2_idx
	on code_session_internal_events (workspace_uuid, code_session_uuid, sequence_num desc)
	where deleted_at is null and agent_id is null and is_compaction;
create index code_session_internal_events_subagent_compaction_v2_idx
	on code_session_internal_events (workspace_uuid, code_session_uuid, agent_id, sequence_num desc)
	where deleted_at is null and agent_id is not null and is_compaction;
create index environment_work_environment_created_v2_idx
	on environment_work (workspace_uuid, environment_uuid, created_at desc, uuid desc)
	where deleted_at is null;
create index environment_work_poll_v2_idx
	on environment_work (workspace_uuid, environment_uuid, state, created_at, uuid)
	where deleted_at is null;
create index environment_sandboxes_work_v2_idx
	on environment_sandboxes (workspace_uuid, work_uuid)
	where work_uuid is not null;

-- +goose Down

alter table code_sessions add column organization_id bigint, add column workspace_id bigint, add column session_id bigint, add column environment_id bigint;
alter table code_session_inbound_events add column organization_id bigint, add column workspace_id bigint, add column code_session_id bigint;
alter table code_session_outbound_events add column organization_id bigint, add column workspace_id bigint, add column code_session_id bigint;
alter table code_session_internal_events add column organization_id bigint, add column workspace_id bigint, add column code_session_id bigint;
alter table environment_work add column organization_id bigint, add column workspace_id bigint, add column environment_id bigint;
alter table environment_worker_polls add column organization_id bigint, add column workspace_id bigint, add column environment_id bigint;
alter table environment_sandboxes add column organization_id bigint, add column workspace_id bigint, add column environment_id bigint, add column work_id bigint;
alter table jobs add column workspace_id bigint;
alter table workspace_storage_usage add column workspace_id bigint;

update code_sessions r set organization_id = o.id, workspace_id = w.id, session_id = s.id, environment_id = e.id
from organizations o, workspaces w, sessions s, environments e
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and s.uuid = r.session_uuid and e.uuid = r.environment_uuid;
update code_session_inbound_events r set organization_id = o.id, workspace_id = w.id, code_session_id = c.id
from organizations o, workspaces w, code_sessions c
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and c.uuid = r.code_session_uuid;
update code_session_outbound_events r set organization_id = o.id, workspace_id = w.id, code_session_id = c.id
from organizations o, workspaces w, code_sessions c
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and c.uuid = r.code_session_uuid;
update code_session_internal_events r set organization_id = o.id, workspace_id = w.id, code_session_id = c.id
from organizations o, workspaces w, code_sessions c
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and c.uuid = r.code_session_uuid;
update environment_work r set organization_id = o.id, workspace_id = w.id, environment_id = e.id
from organizations o, workspaces w, environments e
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and e.uuid = r.environment_uuid;
update environment_worker_polls r set organization_id = o.id, workspace_id = w.id, environment_id = e.id
from organizations o, workspaces w, environments e
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and e.uuid = r.environment_uuid;
update environment_sandboxes r
set organization_id = o.id,
	workspace_id = w.id,
	environment_id = e.id,
	work_id = (
		select q.id from environment_work q where q.uuid = r.work_uuid
	)
from organizations o, workspaces w, environments e
where o.uuid = r.organization_uuid and w.uuid = r.workspace_uuid and e.uuid = r.environment_uuid;
update jobs r set workspace_id = w.id from workspaces w where w.uuid = r.workspace_uuid;
update workspace_storage_usage r set workspace_id = w.id from workspaces w where w.uuid = r.workspace_uuid;

alter table code_sessions alter column organization_id set not null, alter column workspace_id set not null, alter column session_id set not null, alter column environment_id set not null;
alter table code_session_inbound_events alter column organization_id set not null, alter column workspace_id set not null, alter column code_session_id set not null;
alter table code_session_outbound_events alter column organization_id set not null, alter column workspace_id set not null, alter column code_session_id set not null;
alter table code_session_internal_events alter column organization_id set not null, alter column workspace_id set not null, alter column code_session_id set not null;
alter table environment_work alter column organization_id set not null, alter column workspace_id set not null, alter column environment_id set not null;
alter table environment_worker_polls alter column organization_id set not null, alter column workspace_id set not null, alter column environment_id set not null;
alter table environment_sandboxes alter column organization_id set not null, alter column workspace_id set not null, alter column environment_id set not null;
alter table jobs alter column workspace_id set not null;
alter table workspace_storage_usage alter column workspace_id set not null;

alter table code_sessions drop column organization_uuid, drop column workspace_uuid, drop column session_uuid, drop column environment_uuid;
alter table code_session_inbound_events drop column organization_uuid, drop column workspace_uuid, drop column code_session_uuid;
alter table code_session_outbound_events drop column organization_uuid, drop column workspace_uuid, drop column code_session_uuid;
alter table code_session_internal_events drop column organization_uuid, drop column workspace_uuid, drop column code_session_uuid;
alter table environment_work drop column organization_uuid, drop column workspace_uuid, drop column environment_uuid;
alter table environment_worker_polls drop column organization_uuid, drop column workspace_uuid, drop column environment_uuid;
alter table environment_sandboxes drop column organization_uuid, drop column workspace_uuid, drop column environment_uuid, drop column work_uuid;
alter table jobs drop column workspace_uuid;
alter table workspace_storage_usage drop column workspace_uuid;

alter table code_sessions add constraint code_sessions_workspace_external_id_key unique (workspace_id, external_id);
alter table code_session_inbound_events add constraint code_session_inbound_events_workspace_external_id_key unique (workspace_id, external_id);
alter table code_session_outbound_events add constraint code_session_outbound_events_workspace_external_id_key unique (workspace_id, external_id);
alter table code_session_internal_events add constraint code_session_internal_events_workspace_external_id_key unique (workspace_id, external_id);
alter table environment_work add constraint environment_work_workspace_external_id_key unique (workspace_id, external_id);
alter table environment_worker_polls add constraint environment_worker_polls_environment_worker_key unique (environment_id, worker_id);
alter table workspace_storage_usage add constraint workspace_storage_usage_pk primary key (workspace_id);

create index code_sessions_public_session_v1_idx on code_sessions (workspace_id, session_external_id) where deleted_at is null;
create unique index code_session_inbound_events_sequence_v1_key on code_session_inbound_events (workspace_id, code_session_external_id, sequence_num) where deleted_at is null;
create unique index code_session_inbound_events_idempotency_v1_key on code_session_inbound_events (workspace_id, idempotency_key) where deleted_at is null and idempotency_key <> '';
create index code_session_inbound_events_queued_v1_idx on code_session_inbound_events (workspace_id, code_session_external_id, sequence_num asc) where deleted_at is null and delivery_status = 'queued';
create unique index code_session_outbound_events_sequence_v1_key on code_session_outbound_events (workspace_id, code_session_external_id, sequence_num) where deleted_at is null;
create unique index code_session_outbound_events_idempotency_v1_key on code_session_outbound_events (workspace_id, idempotency_key) where deleted_at is null and idempotency_key <> '';
create index code_session_outbound_events_created_v1_idx on code_session_outbound_events (workspace_id, code_session_external_id, created_at asc, id asc) where deleted_at is null;
create unique index code_session_internal_events_sequence_v1_key on code_session_internal_events (workspace_id, code_session_external_id, sequence_num) where deleted_at is null;
create unique index code_session_internal_events_idempotency_v1_key on code_session_internal_events (workspace_id, idempotency_key) where deleted_at is null and idempotency_key <> '';
create index code_session_internal_events_foreground_list_v1_idx on code_session_internal_events (workspace_id, code_session_external_id, sequence_num asc) where deleted_at is null and agent_id is null;
create index code_session_internal_events_subagent_list_v1_idx on code_session_internal_events (workspace_id, code_session_external_id, agent_id, sequence_num asc) where deleted_at is null and agent_id is not null;
create index code_session_internal_events_foreground_compaction_v1_idx on code_session_internal_events (workspace_id, code_session_external_id, sequence_num desc) where deleted_at is null and agent_id is null and is_compaction;
create index code_session_internal_events_subagent_compaction_v1_idx on code_session_internal_events (workspace_id, code_session_external_id, agent_id, sequence_num desc) where deleted_at is null and agent_id is not null and is_compaction;
create index environment_work_environment_created_v1_idx on environment_work (workspace_id, environment_external_id, created_at desc, id desc) where deleted_at is null;
create index environment_work_poll_v1_idx on environment_work (workspace_id, environment_external_id, state, created_at, id) where deleted_at is null;
create index environment_sandboxes_work_v1_idx on environment_sandboxes (workspace_id, work_external_id) where work_external_id is not null;
