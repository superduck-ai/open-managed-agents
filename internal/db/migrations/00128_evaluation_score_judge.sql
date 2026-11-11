-- +goose Up
ALTER TABLE evaluation_task_scores
    ADD COLUMN IF NOT EXISTS judge_model text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS judge_prompt_version text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS evaluation_tasks_running_idx
    ON evaluation_tasks (updated_at, uuid)
    WHERE status = 'running';

CREATE INDEX IF NOT EXISTS evaluation_task_items_running_idx
    ON evaluation_task_items (updated_at, uuid)
    WHERE status = 'running';

-- +goose Down
DROP INDEX IF EXISTS evaluation_task_items_running_idx;
DROP INDEX IF EXISTS evaluation_tasks_running_idx;
ALTER TABLE evaluation_task_scores
    DROP COLUMN IF EXISTS judge_prompt_version,
    DROP COLUMN IF EXISTS judge_model;
