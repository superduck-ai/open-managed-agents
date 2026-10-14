-- +goose Up
alter table llm_providers alter column workspace_uuid drop not null;

create unique index llm_providers_organization_default_key
	on llm_providers (organization_uuid)
	where workspace_uuid is null;

-- +goose Down
drop index llm_providers_organization_default_key;

delete from llm_providers where workspace_uuid is null;

alter table llm_providers alter column workspace_uuid set not null;
