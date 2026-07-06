-- +goose Up
alter table code_sessions
	add column if not exists current_worker_epoch bigint not null default 0,
	add column if not exists worker_lease_expires_at timestamptz,
	add column if not exists worker_registered_at timestamptz,
	add column if not exists worker_last_heartbeat_at timestamptz,
	add column if not exists worker_token_session_id text,
	add column if not exists worker_binding jsonb not null default '{}'::jsonb;

create index if not exists code_sessions_worker_lease_expiry_v1_idx
	on code_sessions (worker_lease_expires_at)
	where deleted_at is null and worker_lease_expires_at is not null;

-- +goose Down
drop index if exists code_sessions_worker_lease_expiry_v1_idx;

alter table code_sessions
	drop column if exists worker_binding,
	drop column if exists worker_token_session_id,
	drop column if exists worker_last_heartbeat_at,
	drop column if exists worker_registered_at,
	drop column if exists worker_lease_expires_at,
	drop column if exists current_worker_epoch;
