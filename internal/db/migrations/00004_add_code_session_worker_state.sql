-- +goose Up
alter table if exists code_sessions
	add column if not exists worker_status text;

update code_sessions
set worker_status = 'idle'
where worker_status is null;

alter table if exists code_sessions
	alter column worker_status set default 'idle';

alter table if exists code_sessions
	alter column worker_status set not null;

alter table if exists code_sessions
	add column if not exists worker_external_metadata jsonb;

update code_sessions
set worker_external_metadata = '{}'::jsonb
where worker_external_metadata is null;

alter table if exists code_sessions
	alter column worker_external_metadata set default '{}'::jsonb;

alter table if exists code_sessions
	alter column worker_external_metadata set not null;

alter table if exists code_sessions
	add column if not exists worker_requires_action_details jsonb;

alter table if exists code_sessions
	drop constraint if exists code_sessions_worker_status_check;

alter table if exists code_sessions
	add constraint code_sessions_worker_status_check
	check (worker_status in ('idle', 'running', 'requires_action'));

-- +goose Down
alter table if exists code_sessions
	drop constraint if exists code_sessions_worker_status_check;

alter table if exists code_sessions
	drop column if exists worker_requires_action_details;

alter table if exists code_sessions
	drop column if exists worker_external_metadata;

alter table if exists code_sessions
	drop column if exists worker_status;
