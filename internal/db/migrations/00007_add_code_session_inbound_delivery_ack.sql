-- +goose Up
alter table code_session_inbound_events
	add column if not exists delivery_worker_epoch bigint,
	add column if not exists received_at timestamptz,
	add column if not exists processing_at timestamptz,
	add column if not exists processed_at timestamptz,
	add column if not exists last_delivery_attempt_at timestamptz,
	add column if not exists last_delivery_update_at timestamptz,
	add column if not exists delivery_attempts integer not null default 0;

create index if not exists code_session_inbound_events_payload_uuid_v1_idx
	on code_session_inbound_events (code_session_id, payload_uuid, sequence_num asc)
	where deleted_at is null and payload_uuid is not null;

create index if not exists code_session_inbound_events_unprocessed_v1_idx
	on code_session_inbound_events (code_session_external_id, sequence_num asc)
	where deleted_at is null and delivery_status <> 'processed';

-- +goose Down
drop index if exists code_session_inbound_events_unprocessed_v1_idx;
drop index if exists code_session_inbound_events_payload_uuid_v1_idx;

alter table code_session_inbound_events
	drop column if exists delivery_attempts,
	drop column if exists last_delivery_update_at,
	drop column if exists last_delivery_attempt_at,
	drop column if exists processed_at,
	drop column if exists processing_at,
	drop column if exists received_at,
	drop column if exists delivery_worker_epoch;
