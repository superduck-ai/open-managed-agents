-- +goose Up
CREATE TABLE evaluation_rubrics (
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
CREATE INDEX evaluation_rubrics_workspace_created_idx
    ON evaluation_rubrics (organization_uuid, workspace_uuid, created_at DESC, uuid DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE evaluation_rubric_versions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    rubric_uuid uuid NOT NULL,
    version integer NOT NULL CHECK (version >= 1),
    body text NOT NULL,
    pass_score integer NOT NULL CHECK (pass_score >= 0 AND pass_score <= 100),
    model text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (rubric_uuid, version)
);

-- +goose Down
DROP TABLE IF EXISTS evaluation_rubric_versions;
DROP TABLE IF EXISTS evaluation_rubrics;
