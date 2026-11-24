-- +goose Up
ALTER TABLE cloud_harness_turns
    ADD COLUMN subagent_thread_uuid uuid,
    ADD COLUMN delegation_tool_uuid uuid,
    DROP CONSTRAINT cloud_harness_turn_task_check,
    ADD CONSTRAINT cloud_harness_turn_task_check CHECK (
        ((task_uuid IS NULL AND tool_uuid IS NULL AND task_kind = '')
            OR (task_uuid IS NOT NULL AND tool_uuid IS NOT NULL AND task_kind IN ('tool', 'model', 'subagent')))
        AND ((subagent_thread_uuid IS NULL AND delegation_tool_uuid IS NULL AND task_kind <> 'subagent')
             OR (subagent_thread_uuid IS NOT NULL AND delegation_tool_uuid IS NOT NULL))
    );

ALTER TABLE cloud_harness_tools ADD COLUMN delegation_tool_uuid uuid;

-- +goose Down
ALTER TABLE cloud_harness_tools DROP COLUMN delegation_tool_uuid;

ALTER TABLE cloud_harness_turns
    ADD CONSTRAINT cloud_harness_subagent_drain_check CHECK (subagent_thread_uuid IS NULL);

ALTER TABLE cloud_harness_turns
    DROP CONSTRAINT cloud_harness_turn_task_check,
    DROP COLUMN subagent_thread_uuid,
    DROP COLUMN delegation_tool_uuid,
    ADD CONSTRAINT cloud_harness_turn_task_check CHECK (
        (task_uuid IS NULL AND tool_uuid IS NULL AND task_kind = '')
        OR (task_uuid IS NOT NULL AND tool_uuid IS NOT NULL AND task_kind IN ('tool', 'model'))
    );
