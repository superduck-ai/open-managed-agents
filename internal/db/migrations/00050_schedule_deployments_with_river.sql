-- +goose Up
alter table deployments
	add column schedule_revision bigint not null default 0,
	add column next_scheduled_at timestamptz;

alter table deployment_runs
	add column scheduled_at timestamptz;

update deployment_runs
set scheduled_at = (trigger_context ->> 'scheduled_at')::timestamptz
where trigger_type = 'schedule';

alter table deployment_runs
	add constraint deployment_runs_scheduled_at_check check (
		(trigger_type = 'schedule' and scheduled_at is not null)
		or (trigger_type <> 'schedule' and scheduled_at is null)
	);

create unique index deployment_runs_schedule_occurrence_idx
	on deployment_runs (deployment_uuid, scheduled_at)
	where trigger_type = 'schedule';

create index deployments_pending_schedule_idx
	on deployments (next_scheduled_at)
	where status = 'active'
		and archived_at is null
		and deleted_at is null
		and next_scheduled_at is not null;

alter table deployment_runs
	drop column trigger_context;

-- +goose Down
drop index deployments_pending_schedule_idx;
drop index deployment_runs_schedule_occurrence_idx;

alter table deployment_runs
	add column trigger_context jsonb;

update deployment_runs
set trigger_context = case
	when trigger_type = 'schedule' then jsonb_build_object('type', 'schedule', 'scheduled_at', scheduled_at)
	else jsonb_build_object('type', 'manual')
end;

alter table deployment_runs
	alter column trigger_context set not null,
	drop column scheduled_at;

alter table deployments
	drop column next_scheduled_at,
	drop column schedule_revision;
