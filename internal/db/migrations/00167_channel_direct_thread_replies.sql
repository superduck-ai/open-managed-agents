-- +goose Up
ALTER TABLE channels ADD COLUMN direct_reply_in_thread boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE channels DROP COLUMN direct_reply_in_thread;
