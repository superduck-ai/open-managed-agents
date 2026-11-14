-- +goose Up
ALTER TABLE code_sessions
    ADD COLUMN IF NOT EXISTS subagent_published_sequence_num bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE code_sessions DROP COLUMN IF EXISTS subagent_published_sequence_num;
