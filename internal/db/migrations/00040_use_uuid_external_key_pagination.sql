-- +goose Up

drop index if exists external_keys_organization_created_v1_idx;

create index external_keys_organization_created_v1_idx
	on external_keys (organization_uuid, created_at desc, uuid desc)
	where deleted_at is null;

-- +goose Down

drop index if exists external_keys_organization_created_v1_idx;

create index external_keys_organization_created_v1_idx
	on external_keys (organization_uuid, created_at desc, id desc)
	where deleted_at is null;
