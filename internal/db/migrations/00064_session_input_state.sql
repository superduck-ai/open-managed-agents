-- +goose Up
ALTER TABLE code_sessions ADD COLUMN worker_turn_started boolean NOT NULL DEFAULT false;
UPDATE code_sessions SET worker_turn_started = true WHERE worker_status IN ('running', 'requires_action');

ALTER TABLE session_events ALTER COLUMN processed_at DROP NOT NULL;

-- +goose Down
-- Refuse rollback while inputs are queued; never mark undelivered work processed.
ALTER TABLE session_events ALTER COLUMN processed_at SET NOT NULL;

ALTER TABLE code_sessions DROP COLUMN worker_turn_started;
