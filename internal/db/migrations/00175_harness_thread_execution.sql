-- +goose Up
ALTER TABLE harness_thread_state
    ADD COLUMN execution_input_uuid uuid,
    ADD COLUMN execution jsonb,
    ADD CONSTRAINT harness_thread_execution_check CHECK (
        (execution IS NULL AND execution_input_uuid IS NULL) OR (
            execution IS NOT NULL AND execution_input_uuid IS NOT NULL AND thread_uuid = session_uuid
            AND execution ?& ARRAY['state', 'worker_epoch', 'attempts', 'task_kind']
            AND COALESCE(execution->>'state', '') IN ('pending', 'running', 'waiting_choice', 'waiting_tool', 'completed', 'failed', 'interrupted')
            AND (
                (execution->>'task_uuid' IS NULL AND execution->>'tool_uuid' IS NULL
                    AND COALESCE(execution->>'task_kind', 'invalid') = '')
                OR (execution->>'task_uuid' IS NOT NULL AND execution->>'tool_uuid' IS NOT NULL
                    AND COALESCE(execution->>'task_kind', '') IN ('tool', 'model', 'subagent'))
            )
            AND (
                (execution->>'subagent_thread_uuid' IS NULL AND execution->>'delegation_tool_uuid' IS NULL
                    AND execution->>'task_kind' <> 'subagent')
                OR (execution->>'subagent_thread_uuid' IS NOT NULL AND execution->>'delegation_tool_uuid' IS NOT NULL)
            )
        )
    );

CREATE INDEX harness_thread_wake_pending_idx ON harness_thread_state (id)
    WHERE execution->>'wake_pending' = 'true';

DROP INDEX session_event_harness_task_idx;
ALTER TABLE session_events
    DROP CONSTRAINT session_event_harness_execution_input,
    DROP COLUMN harness_execution;

-- +goose Down
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

DROP INDEX harness_thread_wake_pending_idx;

ALTER TABLE harness_thread_state
    DROP CONSTRAINT harness_thread_execution_check,
    DROP COLUMN execution,
    DROP COLUMN execution_input_uuid;
