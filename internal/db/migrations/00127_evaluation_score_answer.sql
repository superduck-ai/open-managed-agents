-- +goose Up
ALTER TABLE evaluation_task_scores
    ADD COLUMN IF NOT EXISTS answer text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE evaluation_task_scores DROP COLUMN IF EXISTS answer;
