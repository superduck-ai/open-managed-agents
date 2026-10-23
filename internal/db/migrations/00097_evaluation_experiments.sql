-- +goose Up

create table if not exists evaluation_experiments (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	name text not null,
	description text not null default '',
	dataset_uuid uuid not null,
	dataset_external_id text not null,
	version_uuid uuid not null,
	version_external_id text not null,
	agent_external_id text not null,
	agent_version integer not null,
	environment_external_id text not null,
	timeout_seconds integer not null,
	env jsonb,
	resources jsonb,
	evaluator_set_key text not null,
	status text not null,
	created_by_user_uuid uuid,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint evaluation_experiments_id_pk primary key (id),
	constraint evaluation_experiments_uuid_key unique (uuid),
	constraint evaluation_experiments_external_id_key unique (external_id),
	constraint evaluation_experiments_name_key unique (organization_uuid, workspace_uuid, name),
	constraint evaluation_experiments_name_length check (char_length(name) between 1 and 255),
	constraint evaluation_experiments_description_length check (char_length(description) <= 1024),
	constraint evaluation_experiments_agent_length check (char_length(agent_external_id) between 1 and 255),
	constraint evaluation_experiments_environment_length check (char_length(environment_external_id) between 1 and 255),
	constraint evaluation_experiments_agent_version_positive check (agent_version > 0),
	constraint evaluation_experiments_timeout_range check (timeout_seconds between 1 and 86400),
	constraint evaluation_experiments_env_object check (env is null or jsonb_typeof(env) = 'object'),
	constraint evaluation_experiments_resources_array check (resources is null or jsonb_typeof(resources) = 'array'),
	constraint evaluation_experiments_status_known check (status in ('pending', 'running', 'completed', 'failed', 'cancelled'))
);

create index if not exists evaluation_experiments_workspace_created_idx
	on evaluation_experiments (organization_uuid, workspace_uuid, created_at desc, uuid desc);

create index if not exists evaluation_experiments_compare_idx
	on evaluation_experiments (organization_uuid, workspace_uuid, version_external_id, evaluator_set_key, status);

create table if not exists evaluation_experiment_evaluators (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	experiment_uuid uuid not null,
	position integer not null,
	evaluator_uuid uuid not null,
	evaluator_external_id text not null,
	version_uuid uuid not null,
	version_external_id text not null,
	created_at timestamptz not null default now(),
	constraint evaluation_experiment_evaluators_id_pk primary key (id),
	constraint evaluation_experiment_evaluators_uuid_key unique (uuid),
	constraint evaluation_experiment_evaluators_external_id_key unique (external_id),
	constraint evaluation_experiment_evaluators_position_positive check (position > 0),
	constraint evaluation_experiment_evaluators_position_key unique (experiment_uuid, position),
	constraint evaluation_experiment_evaluators_evaluator_key unique (experiment_uuid, evaluator_uuid)
);

create index if not exists evaluation_experiment_evaluators_experiment_idx
	on evaluation_experiment_evaluators (organization_uuid, workspace_uuid, experiment_uuid, position asc);

create table if not exists evaluation_experiment_items (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	experiment_uuid uuid not null,
	case_uuid uuid not null,
	case_external_id text not null,
	position integer not null,
	session_external_id text,
	status text not null,
	error_message text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint evaluation_experiment_items_id_pk primary key (id),
	constraint evaluation_experiment_items_uuid_key unique (uuid),
	constraint evaluation_experiment_items_external_id_key unique (external_id),
	constraint evaluation_experiment_items_position_positive check (position > 0),
	constraint evaluation_experiment_items_position_key unique (experiment_uuid, position),
	constraint evaluation_experiment_items_case_key unique (experiment_uuid, case_uuid),
	constraint evaluation_experiment_items_error_length check (char_length(error_message) <= 1024),
	constraint evaluation_experiment_items_status_known check (status in ('pending', 'running', 'completed', 'failed', 'cancelled'))
);

create index if not exists evaluation_experiment_items_experiment_idx
	on evaluation_experiment_items (organization_uuid, workspace_uuid, experiment_uuid, position asc, uuid asc);

-- +goose Down

drop table if exists evaluation_experiment_items;
drop table if exists evaluation_experiment_evaluators;
drop table if exists evaluation_experiments;
