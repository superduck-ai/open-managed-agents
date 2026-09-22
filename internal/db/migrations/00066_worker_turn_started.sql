-- +goose Up
ALTER TABLE code_sessions ADD COLUMN worker_turn_started boolean NOT NULL DEFAULT false;
UPDATE code_sessions SET worker_turn_started = true WHERE worker_status IN ('running', 'requires_action');

-- +goose Down
ALTER TABLE code_sessions DROP COLUMN worker_turn_started;
