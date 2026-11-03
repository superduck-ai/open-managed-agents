-- +goose Up

create table if not exists evaluation_evaluators (
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
	constraint evaluation_evaluators_id_pk primary key (id),
	constraint evaluation_evaluators_uuid_key unique (uuid),
	constraint evaluation_evaluators_external_id_key unique (external_id),
	constraint evaluation_evaluators_name_length check (char_length(name) between 1 and 255),
	constraint evaluation_evaluators_description_length check (char_length(description) <= 1024)
);

create index if not exists evaluation_evaluators_workspace_created_idx
	on evaluation_evaluators (organization_uuid, workspace_uuid, created_at desc, uuid desc)
	where archived_at is null;

create table if not exists evaluation_evaluator_versions (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	evaluator_uuid uuid not null,
	version integer not null,
	evaluator_type text not null,
	model text not null default '',
	agent_id text not null default '',
	prompt_template text not null default '',
	variables jsonb not null,
	created_at timestamptz not null default now(),
	constraint evaluation_evaluator_versions_id_pk primary key (id),
	constraint evaluation_evaluator_versions_uuid_key unique (uuid),
	constraint evaluation_evaluator_versions_external_id_key unique (external_id),
	constraint evaluation_evaluator_versions_evaluator_version_key unique (evaluator_uuid, version),
	constraint evaluation_evaluator_versions_version_positive check (version > 0),
	constraint evaluation_evaluator_versions_type_known check (evaluator_type in ('llm', 'agent')),
	constraint evaluation_evaluator_versions_model_length check (char_length(model) <= 255),
	constraint evaluation_evaluator_versions_agent_id_length check (char_length(agent_id) <= 255),
	constraint evaluation_evaluator_versions_prompt_length check (char_length(prompt_template) <= 16000),
	constraint evaluation_evaluator_versions_variables_array check (jsonb_typeof(variables) = 'array'),
	constraint evaluation_evaluator_versions_shape check (
		(
			evaluator_type = 'llm'
			and char_length(model) between 1 and 255
			and agent_id = ''
			and char_length(prompt_template) between 1 and 16000
		)
		or (
			evaluator_type = 'agent'
			and char_length(agent_id) between 1 and 255
			and model = ''
			and prompt_template = ''
		)
	)
);

create index if not exists evaluation_evaluator_versions_evaluator_idx
	on evaluation_evaluator_versions (organization_uuid, workspace_uuid, evaluator_uuid, version desc);

-- +goose Down

drop table if exists evaluation_evaluator_versions;
drop table if exists evaluation_evaluators;
