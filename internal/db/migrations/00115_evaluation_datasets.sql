-- +goose Up

create table if not exists evaluation_datasets (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	name text not null,
	description text not null default '',
	created_by_user_uuid uuid,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	archived_at timestamptz,
	constraint evaluation_datasets_id_pk primary key (id),
	constraint evaluation_datasets_uuid_key unique (uuid),
	constraint evaluation_datasets_external_id_key unique (external_id),
	constraint evaluation_datasets_name_length check (char_length(name) between 1 and 255),
	constraint evaluation_datasets_description_length check (char_length(description) <= 1024)
);

create index if not exists evaluation_datasets_workspace_created_idx
	on evaluation_datasets (organization_uuid, workspace_uuid, created_at desc, uuid desc)
	where archived_at is null;

create table if not exists evaluation_dataset_versions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	dataset_uuid uuid not null,
	version integer not null,
	created_at timestamptz not null default now(),
	constraint evaluation_dataset_versions_id_pk primary key (id),
	constraint evaluation_dataset_versions_uuid_key unique (uuid),
	constraint evaluation_dataset_versions_external_id_key unique (external_id),
	constraint evaluation_dataset_versions_dataset_version_key unique (dataset_uuid, version),
	constraint evaluation_dataset_versions_version_positive check (version > 0)
);

create index if not exists evaluation_dataset_versions_dataset_idx
	on evaluation_dataset_versions (organization_uuid, workspace_uuid, dataset_uuid, version desc);

create table if not exists evaluation_cases (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	dataset_uuid uuid not null,
	version_uuid uuid not null,
	position integer not null,
	name text not null,
	description text not null default '',
	turns jsonb not null,
	checkpoints jsonb,
	input_files jsonb,
	reference_files jsonb,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint evaluation_cases_id_pk primary key (id),
	constraint evaluation_cases_uuid_key unique (uuid),
	constraint evaluation_cases_external_id_key unique (external_id),
	constraint evaluation_cases_position_positive check (position > 0),
	constraint evaluation_cases_name_length check (char_length(name) between 1 and 255),
	constraint evaluation_cases_description_length check (char_length(description) <= 1024),
	constraint evaluation_cases_turns_array check (jsonb_typeof(turns) = 'array'),
	constraint evaluation_cases_checkpoints_shape check (checkpoints is null or jsonb_typeof(checkpoints) in ('array', 'object')),
	constraint evaluation_cases_input_files_array check (input_files is null or jsonb_typeof(input_files) = 'array'),
	constraint evaluation_cases_reference_files_array check (reference_files is null or jsonb_typeof(reference_files) = 'array'),
	constraint evaluation_cases_version_position_key unique (version_uuid, position)
);

create index if not exists evaluation_cases_version_position_idx
	on evaluation_cases (organization_uuid, workspace_uuid, version_uuid, position asc, uuid asc);

-- +goose Down

drop table if exists evaluation_cases;
drop table if exists evaluation_dataset_versions;
drop table if exists evaluation_datasets;
