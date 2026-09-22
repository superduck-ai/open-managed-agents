-- +goose Up
CREATE INDEX session_events_processed_order_idx ON session_events (workspace_uuid, session_external_id, processed_at, id) WHERE deleted_at IS NULL;

ALTER TABLE code_sessions ADD COLUMN worker_turn_started boolean NOT NULL DEFAULT false;
UPDATE code_sessions SET worker_turn_started = true WHERE worker_status IN ('running', 'requires_action');

ALTER TABLE session_events ALTER COLUMN processed_at DROP NOT NULL;

-- +goose Down
UPDATE session_events SET processed_at = created_at WHERE processed_at IS NULL;
ALTER TABLE session_events ALTER COLUMN processed_at SET NOT NULL;

ALTER TABLE code_sessions DROP COLUMN worker_turn_started;
DROP INDEX IF EXISTS session_events_processed_order_idx;
