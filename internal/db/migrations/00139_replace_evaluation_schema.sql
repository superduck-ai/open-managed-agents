-- +goose Up
DROP TABLE IF EXISTS evaluation_experiment_scores;
DROP TABLE IF EXISTS evaluation_experiment_items;
DROP TABLE IF EXISTS evaluation_experiment_evaluators;
DROP TABLE IF EXISTS evaluation_experiments;
DROP TABLE IF EXISTS evaluation_evaluator_versions;
DROP TABLE IF EXISTS evaluation_evaluators;

CREATE TABLE evaluation_tasks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    created_by_user_uuid uuid,
    created_by_api_key_uuid uuid,
    name text NOT NULL,
    mode text NOT NULL CHECK (mode IN ('evaluate', 'iterate')),
    status text NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    agent_external_id text NOT NULL,
    agent_version integer NOT NULL CHECK (agent_version >= 1),
    environment_external_id text NOT NULL,
    dataset_external_id text NOT NULL,
    dataset_version_external_id text NOT NULL,
    rubric_external_id text NOT NULL,
    rubric_version_external_id text NOT NULL,
    pass_score integer NOT NULL CHECK (pass_score >= 0 AND pass_score <= 100),
    rubric_body text NOT NULL,
    rubric_model text NOT NULL DEFAULT '',
    strategy text NOT NULL DEFAULT '' CHECK (strategy IN ('', 'safe', 'targeted', 'deep')),
    low_score_threshold integer NOT NULL DEFAULT 60 CHECK (low_score_threshold >= 0 AND low_score_threshold <= 100),
    optimize_system boolean NOT NULL DEFAULT false,
    baseline_system text NOT NULL DEFAULT '',
    candidate_system text NOT NULL DEFAULT '',
    candidate_summary text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    vault_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    resources jsonb NOT NULL DEFAULT '[]'::jsonb,
    applied_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX evaluation_tasks_workspace_created_idx
    ON evaluation_tasks (organization_uuid, workspace_uuid, created_at DESC, uuid DESC);

CREATE TABLE evaluation_task_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    task_uuid uuid NOT NULL,
    task_external_id text NOT NULL,
    case_external_id text NOT NULL DEFAULT '',
    round integer NOT NULL CHECK (round >= 1),
    position integer NOT NULL CHECK (position >= 1),
    session_external_id text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    error_message text NOT NULL DEFAULT '',
    query text NOT NULL,
    expected_answer text NOT NULL DEFAULT '',
    expected_tool_trace jsonb,
    acceptance text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX evaluation_task_items_task_idx
    ON evaluation_task_items (task_uuid, round, position);
CREATE INDEX evaluation_task_items_session_idx
    ON evaluation_task_items (organization_uuid, workspace_uuid, session_external_id)
    WHERE session_external_id <> '';

CREATE TABLE evaluation_task_scores (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    task_uuid uuid NOT NULL,
    item_uuid uuid NOT NULL UNIQUE,
    status text NOT NULL CHECK (status IN ('completed', 'failed')),
    score double precision NOT NULL DEFAULT 0,
    passed boolean NOT NULL DEFAULT false,
    explanation text NOT NULL DEFAULT '',
    dimensions jsonb NOT NULL DEFAULT '[]'::jsonb,
    error_message text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS evaluation_task_scores;
DROP TABLE IF EXISTS evaluation_task_items;
DROP TABLE IF EXISTS evaluation_tasks;
