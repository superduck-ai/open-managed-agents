-- +goose Up
alter table if exists code_sessions
	alter column current_worker_epoch set default 0;

update code_sessions
set current_worker_epoch = 0
where current_worker_epoch is null or current_worker_epoch < 0;

alter table if exists code_sessions
	alter column current_worker_epoch set not null;

-- +goose Down
select 1;
