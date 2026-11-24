-- +goose Up
ALTER TABLE harness_thread_state DROP COLUMN processed_message_ids;

-- +goose Down
ALTER TABLE harness_thread_state ADD COLUMN processed_message_ids json NOT NULL DEFAULT '{}';
