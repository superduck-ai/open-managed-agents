-- +goose Up
CREATE TABLE evaluation_datasets (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX evaluation_datasets_workspace_created_idx
    ON evaluation_datasets (organization_uuid, workspace_uuid, created_at DESC, uuid DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE evaluation_dataset_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    dataset_uuid uuid NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dataset_uuid, version)
);

CREATE TABLE evaluation_cases (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    dataset_uuid uuid NOT NULL,
    version_uuid uuid NOT NULL,
    position integer NOT NULL CHECK (position >= 1),
    query text NOT NULL,
    expected_answer text NOT NULL DEFAULT '',
    expected_tool_trace jsonb,
    source_session_id text NOT NULL DEFAULT '',
    polarity text NOT NULL DEFAULT '' CHECK (polarity IN ('', 'positive', 'negative')),
    acceptance text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX evaluation_cases_version_position_idx
    ON evaluation_cases (version_uuid, position)
    WHERE deleted_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS evaluation_cases;
DROP TABLE IF EXISTS evaluation_dataset_versions;
DROP TABLE IF EXISTS evaluation_datasets;
