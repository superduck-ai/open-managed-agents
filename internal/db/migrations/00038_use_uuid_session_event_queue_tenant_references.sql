-- +goose Up

-- Queue ownership must survive tenant identity remapping during data moves.
create table session_event_queue_uuid_refs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	session_uuid uuid not null,
	session_event_uuid uuid not null,
	created_at timestamptz not null default now(),
	constraint session_event_queue_uuid_refs_id_pk primary key (id),
	constraint session_event_queue_uuid_refs_uuid_key unique (uuid),
	constraint session_event_queue_uuid_refs_event_uuid_key unique (session_event_uuid)
);

insert into session_event_queue_uuid_refs (
	id, uuid, organization_uuid, workspace_uuid, session_uuid, session_event_uuid, created_at
)
overriding system value
select q.id, q.uuid, o.uuid, w.uuid, q.session_uuid, q.session_event_uuid, q.created_at
from session_event_queue q
join organizations o on o.id = q.organization_id
join workspaces w on w.id = q.workspace_id and w.organization_id = o.id
order by q.id;

-- +goose StatementBegin
do $$
begin
	if (select count(*) from session_event_queue_uuid_refs) <>
		(select count(*) from session_event_queue) then
		raise exception 'cannot migrate session event queue tenant references to UUID';
	end if;
end $$;
-- +goose StatementEnd

select setval(
	pg_get_serial_sequence('session_event_queue_uuid_refs', 'id'),
	coalesce((select max(id) from session_event_queue_uuid_refs), 1),
	exists (select 1 from session_event_queue_uuid_refs)
);

drop table session_event_queue;
alter table session_event_queue_uuid_refs rename to session_event_queue;
alter table session_event_queue rename constraint session_event_queue_uuid_refs_id_pk
	to session_event_queue_id_pk;
alter table session_event_queue rename constraint session_event_queue_uuid_refs_uuid_key
	to session_event_queue_uuid_key;
alter table session_event_queue rename constraint session_event_queue_uuid_refs_event_uuid_key
	to session_event_queue_session_event_uuid_key;

create index session_event_queue_session_order_v2_idx
	on session_event_queue (organization_uuid, workspace_uuid, session_uuid, id asc);

-- +goose Down

create table session_event_queue_identity_refs (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_id bigint not null,
	workspace_id bigint not null,
	session_uuid uuid not null,
	session_event_uuid uuid not null,
	created_at timestamptz not null default now(),
	constraint session_event_queue_identity_refs_id_pk primary key (id),
	constraint session_event_queue_identity_refs_uuid_key unique (uuid),
	constraint session_event_queue_identity_refs_event_uuid_key unique (session_event_uuid)
);

insert into session_event_queue_identity_refs (
	id, uuid, organization_id, workspace_id, session_uuid, session_event_uuid, created_at
)
overriding system value
select q.id, q.uuid, o.id, w.id, q.session_uuid, q.session_event_uuid, q.created_at
from session_event_queue q
join organizations o on o.uuid = q.organization_uuid
join workspaces w on w.uuid = q.workspace_uuid and w.organization_id = o.id
order by q.id;

-- +goose StatementBegin
do $$
begin
	if (select count(*) from session_event_queue_identity_refs) <>
		(select count(*) from session_event_queue) then
		raise exception 'cannot restore session event queue tenant identity references';
	end if;
end $$;
-- +goose StatementEnd

select setval(
	pg_get_serial_sequence('session_event_queue_identity_refs', 'id'),
	coalesce((select max(id) from session_event_queue_identity_refs), 1),
	exists (select 1 from session_event_queue_identity_refs)
);

drop table session_event_queue;
alter table session_event_queue_identity_refs rename to session_event_queue;
alter table session_event_queue rename constraint session_event_queue_identity_refs_id_pk
	to session_event_queue_id_pk;
alter table session_event_queue rename constraint session_event_queue_identity_refs_uuid_key
	to session_event_queue_uuid_key;
alter table session_event_queue rename constraint session_event_queue_identity_refs_event_uuid_key
	to session_event_queue_session_event_uuid_key;

create index session_event_queue_session_order_v1_idx
	on session_event_queue (organization_id, workspace_id, session_uuid, id asc);
