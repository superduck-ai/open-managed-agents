-- +goose Up
CREATE INDEX evaluation_task_scores_task_idx
    ON evaluation_task_scores (organization_uuid, workspace_uuid, task_uuid);

-- +goose Down
DROP INDEX IF EXISTS evaluation_task_scores_task_idx;
