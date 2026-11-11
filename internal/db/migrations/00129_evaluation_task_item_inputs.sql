-- +goose Up
ALTER TABLE evaluation_task_items
    ADD COLUMN IF NOT EXISTS inputs jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE evaluation_task_items DROP COLUMN IF EXISTS inputs;
