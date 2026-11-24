-- +goose Up
ALTER TABLE harness_thread_state DROP COLUMN pending;

-- +goose Down
ALTER TABLE harness_thread_state ADD COLUMN pending json NOT NULL DEFAULT '{}';
