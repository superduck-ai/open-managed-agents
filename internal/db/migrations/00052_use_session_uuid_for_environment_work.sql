-- +goose Up

alter table environment_work add column session_uuid uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from environment_work work
		where jsonb_typeof(work.data) is distinct from 'object'
	) then
		raise exception 'environment_work data must be a JSON object';
	end if;

	if exists (
		select 1
		from environment_work work
		where work.data->>'type' is distinct from 'session'
			or jsonb_typeof(work.data->'id') is distinct from 'string'
			or nullif(work.data->>'id', '') is null
			or exists (
				select 1
				from jsonb_object_keys(work.data) field(key)
				where field.key not in ('id', 'type')
			)
	) then
		raise exception 'environment_work contains non-session or unsupported data';
	end if;

	if exists (
		select 1
		from environment_work work
		where (
			select count(*)
			from sessions session
			where session.organization_uuid = work.organization_uuid
				and session.workspace_uuid = work.workspace_uuid
				and session.environment_uuid = work.environment_uuid
				and session.external_id = work.data->>'id'
		) <> 1
	) then
		raise exception 'environment_work data cannot be uniquely mapped to a Session';
	end if;
end $$;
-- +goose StatementEnd

update environment_work work
set session_uuid = session.uuid
from sessions session
where session.organization_uuid = work.organization_uuid
	and session.workspace_uuid = work.workspace_uuid
	and session.environment_uuid = work.environment_uuid
	and session.external_id = work.data->>'id';

-- +goose StatementBegin
do $$
begin
	if exists (select 1 from environment_work where session_uuid is null) then
		raise exception 'environment_work contains data that cannot be mapped to a Session';
	end if;
end $$;
-- +goose StatementEnd

alter table environment_work alter column session_uuid set not null;

create index environment_work_session_v1_idx
	on environment_work (workspace_uuid, session_uuid)
	where deleted_at is null;

alter table environment_work drop column data;

-- +goose Down

alter table environment_work add column data jsonb;

update environment_work work
set data = jsonb_build_object('id', session.external_id, 'type', 'session')
from sessions session
where session.organization_uuid = work.organization_uuid
	and session.workspace_uuid = work.workspace_uuid
	and session.environment_uuid = work.environment_uuid
	and session.uuid = work.session_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (select 1 from environment_work where data is null) then
		raise exception 'environment_work contains a Session reference that cannot be restored';
	end if;
end $$;
-- +goose StatementEnd

alter table environment_work alter column data set not null;
drop index environment_work_session_v1_idx;
alter table environment_work drop column session_uuid;
