-- +goose Up

create table if not exists evaluation_experiment_scores (
	id bigint generated always as identity,
	uuid uuid not null default gen_random_uuid(),
	external_id text not null,
	organization_uuid uuid not null,
	workspace_uuid uuid not null,
	experiment_uuid uuid not null,
	item_uuid uuid not null,
	item_external_id text not null,
	evaluator_uuid uuid not null,
	evaluator_external_id text not null,
	version_uuid uuid not null,
	version_external_id text not null,
	position integer not null,
	status text not null,
	score double precision,
	explanation text not null default '',
	details jsonb,
	error_message text not null default '',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	constraint evaluation_experiment_scores_id_pk primary key (id),
	constraint evaluation_experiment_scores_uuid_key unique (uuid),
	constraint evaluation_experiment_scores_external_id_key unique (external_id),
	constraint evaluation_experiment_scores_item_version_key unique (item_uuid, version_uuid),
	constraint evaluation_experiment_scores_position_positive check (position > 0),
	constraint evaluation_experiment_scores_status_known check (status in ('completed', 'failed')),
	constraint evaluation_experiment_scores_completed_score check (status <> 'completed' or score is not null),
	constraint evaluation_experiment_scores_explanation_length check (char_length(explanation) <= 4000),
	constraint evaluation_experiment_scores_error_length check (char_length(error_message) <= 1024),
	constraint evaluation_experiment_scores_details_object check (details is null or jsonb_typeof(details) = 'object')
);

create index if not exists evaluation_experiment_scores_experiment_idx
	on evaluation_experiment_scores (organization_uuid, workspace_uuid, experiment_uuid, position asc, uuid asc);

-- +goose Down

drop table if exists evaluation_experiment_scores;
