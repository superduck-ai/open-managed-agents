-- +goose Up
ALTER TABLE evaluation_task_scores
    ADD COLUMN IF NOT EXISTS judge_system text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS judge_prompt text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS judge_response text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE evaluation_task_scores
    DROP COLUMN IF EXISTS judge_response,
    DROP COLUMN IF EXISTS judge_prompt,
    DROP COLUMN IF EXISTS judge_system;
