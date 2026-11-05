-- +goose Up
ALTER TABLE dsh_worker_states
    DROP CONSTRAINT dsh_worker_states_model_control,
    DROP COLUMN model_control_input_id,
    DROP COLUMN model_control_sequence,
    DROP COLUMN model,
    DROP COLUMN trace_turns;

-- +goose Down
ALTER TABLE dsh_worker_states
    ADD COLUMN model text,
    ADD COLUMN trace_turns jsonb,
    ADD COLUMN model_control_sequence numeric(20, 0),
    ADD COLUMN model_control_input_id text,
    ADD CONSTRAINT dsh_worker_states_model_control CHECK (
        (model_control_sequence IS NULL AND model_control_input_id IS NULL)
        OR (model_control_sequence IS NOT NULL AND model_control_sequence BETWEEN 1 AND 18446744073709551615
            AND model_control_input_id IS NOT NULL AND model_control_input_id != '')
    );
