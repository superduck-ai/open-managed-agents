-- +goose Up

alter table external_keys
	add column organization_uuid uuid;

update external_keys ek
set organization_uuid = o.uuid
from organizations o
where o.id = ek.organization_id;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from external_keys
		where organization_uuid is null
	) then
		raise exception 'cannot migrate external key organization references to UUID';
	end if;
end $$;
-- +goose StatementEnd

alter table external_keys
	alter column organization_uuid set not null,
	drop column organization_id;

create index if not exists external_keys_organization_created_v1_idx
	on external_keys (organization_uuid, created_at desc, id desc)
	where deleted_at is null;

-- +goose Down

alter table external_keys
	add column organization_id bigint;

update external_keys ek
set organization_id = o.id
from organizations o
where o.uuid = ek.organization_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (
		select 1
		from external_keys
		where organization_id is null
	) then
		raise exception 'cannot restore external key organization internal references';
	end if;
end $$;
-- +goose StatementEnd

alter table external_keys
	alter column organization_id set not null,
	drop column organization_uuid;

create index if not exists external_keys_organization_created_v1_idx
	on external_keys (organization_id, created_at desc, id desc)
	where deleted_at is null;
