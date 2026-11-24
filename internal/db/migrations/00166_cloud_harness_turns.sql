-- +goose Up
ALTER TABLE sessions ADD COLUMN execution_runtime text NOT NULL DEFAULT '';

CREATE TABLE cloud_harness_turns (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    input_event_uuid uuid NOT NULL,
    response_event_uuid uuid,
    state text NOT NULL DEFAULT 'pending',
    worker_epoch bigint NOT NULL DEFAULT 0,
    lease_until timestamptz,
    published_at timestamptz,
    model_started_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, input_event_uuid)
);

CREATE INDEX cloud_harness_turns_unfinished_idx
    ON cloud_harness_turns (organization_uuid, workspace_uuid, session_uuid, id)
    WHERE state IN ('pending', 'running');

CREATE INDEX cloud_harness_turns_unpublished_idx
    ON cloud_harness_turns (id) WHERE state = 'pending' AND published_at IS NULL;

-- +goose Down
DROP TABLE cloud_harness_turns;
ALTER TABLE sessions DROP COLUMN execution_runtime;
