-- +goose Up
create table session_event_queue (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	session_uuid uuid not null,
	session_event_uuid uuid not null,
	created_at timestamptz not null default now(),
	constraint session_event_queue_id_pk primary key (id),
	constraint session_event_queue_uuid_key unique (uuid),
	constraint session_event_queue_session_event_uuid_key unique (session_event_uuid)
);

create index session_event_queue_session_order_v1_idx
	on session_event_queue (organization_uuid, workspace_uuid, session_uuid, id asc);

-- +goose Down
drop table if exists session_event_queue;
