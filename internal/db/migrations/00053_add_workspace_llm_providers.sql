-- +goose Up
create table llm_providers (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	name text not null,
	base_url text not null,
	api_key_last4 text not null,
	model_ids jsonb not null,
	ciphertext bytea not null,
	nonce bytea not null,
	wrapped_dek bytea not null,
	format_version int not null,
	key_provider text not null,
	key_version bigint not null,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint llm_providers_id_pk primary key (id),
	constraint llm_providers_uuid_key unique (uuid),
	constraint llm_providers_external_id_key unique (external_id),
	constraint llm_providers_organization_workspace_name_key
		unique (organization_uuid, workspace_uuid, name),
	constraint llm_providers_name_length check (char_length(name) between 1 and 100),
	constraint llm_providers_model_ids_array check (jsonb_typeof(model_ids) = 'array')
);

create index llm_providers_organization_workspace_created_idx
	on llm_providers (organization_uuid, workspace_uuid, created_at, uuid);

alter table message_batches add column organization_uuid uuid;

update message_batches batch
set organization_uuid = workspace.organization_uuid
from workspaces workspace
where workspace.uuid = batch.workspace_uuid;

-- +goose StatementBegin
do $$
begin
	if exists (select 1 from message_batches where organization_uuid is null) then
		raise exception 'message_batches contains rows without a matching workspace organization';
	end if;
end $$;
-- +goose StatementEnd

alter table message_batches alter column organization_uuid set not null;

-- +goose Down
alter table message_batches drop column organization_uuid;

drop table llm_providers;
