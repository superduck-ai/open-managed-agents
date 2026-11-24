-- +goose Up
ALTER TABLE cloud_harness_turns
    ADD COLUMN task_uuid uuid,
    ADD COLUMN tool_uuid uuid,
    ADD COLUMN task_kind text NOT NULL DEFAULT '',
    ADD CONSTRAINT cloud_harness_turn_task_check CHECK (
        (task_uuid IS NULL AND tool_uuid IS NULL AND task_kind = '')
        OR (task_uuid IS NOT NULL AND tool_uuid IS NOT NULL AND task_kind IN ('tool', 'model'))
    );

CREATE TABLE cloud_harness_tools (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    turn_uuid uuid NOT NULL,
    call_id text NOT NULL,
    tool_name text NOT NULL,
    arguments jsonb NOT NULL,
    assistant_text text NOT NULL,
    result_text text,
    result_failed boolean NOT NULL DEFAULT false
);

CREATE INDEX cloud_harness_tools_turn_idx
    ON cloud_harness_tools (organization_uuid, workspace_uuid, session_uuid, turn_uuid, id);

-- +goose Down
DROP TABLE cloud_harness_tools;
ALTER TABLE cloud_harness_turns DROP COLUMN task_kind, DROP COLUMN tool_uuid, DROP COLUMN task_uuid;
