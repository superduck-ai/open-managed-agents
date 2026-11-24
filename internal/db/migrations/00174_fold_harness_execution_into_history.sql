-- +goose Up
ALTER TABLE session_events
    ADD COLUMN harness_execution jsonb,
    ADD CONSTRAINT session_event_harness_execution_input CHECK (
        harness_execution IS NULL OR (
            event_type = 'user.message'
            AND harness_execution ?& ARRAY['state', 'worker_epoch', 'attempts', 'task_kind']
            AND COALESCE(harness_execution->>'state', '') IN ('pending', 'running', 'waiting_choice', 'completed', 'failed', 'interrupted')
            AND (
                (harness_execution->>'task_uuid' IS NULL AND harness_execution->>'tool_uuid' IS NULL
                    AND COALESCE(harness_execution->>'task_kind', 'invalid') = '')
                OR (harness_execution->>'task_uuid' IS NOT NULL AND harness_execution->>'tool_uuid' IS NOT NULL
                    AND COALESCE(harness_execution->>'task_kind', '') IN ('tool', 'model', 'subagent'))
            )
            AND (
                (harness_execution->>'subagent_thread_uuid' IS NULL AND harness_execution->>'delegation_tool_uuid' IS NULL
                    AND harness_execution->>'task_kind' <> 'subagent')
                OR (harness_execution->>'subagent_thread_uuid' IS NOT NULL AND harness_execution->>'delegation_tool_uuid' IS NOT NULL)
            )
        )
    );

CREATE INDEX session_event_harness_task_idx
    ON session_events (organization_uuid, workspace_uuid, session_uuid, (harness_execution->>'task_uuid'))
    WHERE harness_execution IS NOT NULL;

ALTER TABLE harness_conversation_history
    ADD COLUMN execution_turn_uuid uuid,
    ADD COLUMN tool_uuid uuid,
    ADD COLUMN tool_result_text text,
    ADD COLUMN tool_result_failed boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT harness_history_tool_scope CHECK ((tool_uuid IS NULL) = (execution_turn_uuid IS NULL));

CREATE UNIQUE INDEX harness_history_tool_uuid_idx
    ON harness_conversation_history (organization_uuid, workspace_uuid, session_uuid, execution_turn_uuid, tool_uuid)
    WHERE tool_uuid IS NOT NULL;

DROP TABLE cloud_harness_tools;
DROP TABLE cloud_harness_turns;

-- +goose Down
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
    model_started_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    task_uuid uuid,
    tool_uuid uuid,
    task_kind text NOT NULL DEFAULT '',
    subagent_thread_uuid uuid,
    delegation_tool_uuid uuid,
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, input_event_uuid),
    CONSTRAINT cloud_harness_turn_task_check CHECK (
        ((task_uuid IS NULL AND tool_uuid IS NULL AND task_kind = '')
            OR (task_uuid IS NOT NULL AND tool_uuid IS NOT NULL AND task_kind IN ('tool', 'model', 'subagent')))
        AND ((subagent_thread_uuid IS NULL AND delegation_tool_uuid IS NULL AND task_kind <> 'subagent')
             OR (subagent_thread_uuid IS NOT NULL AND delegation_tool_uuid IS NOT NULL))
    )
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
    result_failed boolean NOT NULL DEFAULT false,
    delegation_tool_uuid uuid
);

CREATE INDEX cloud_harness_tools_turn_idx
    ON cloud_harness_tools (organization_uuid, workspace_uuid, session_uuid, turn_uuid, id);

DROP INDEX harness_history_tool_uuid_idx;
ALTER TABLE harness_conversation_history
    DROP CONSTRAINT harness_history_tool_scope,
    DROP COLUMN tool_result_failed,
    DROP COLUMN tool_result_text,
    DROP COLUMN tool_uuid,
    DROP COLUMN execution_turn_uuid;

DROP INDEX session_event_harness_task_idx;
ALTER TABLE session_events
    DROP CONSTRAINT session_event_harness_execution_input,
    DROP COLUMN harness_execution;
