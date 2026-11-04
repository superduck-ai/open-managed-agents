-- +goose Up
CREATE TABLE dsh_worker_states (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    model text,
    trace_turns jsonb,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid)
);

-- +goose Down
DROP TABLE dsh_worker_states;
