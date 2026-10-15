-- +goose Up
ALTER TABLE environment_work ADD COLUMN queued_at timestamptz;
ALTER TABLE environment_work ADD COLUMN trace_parent text;
UPDATE environment_work SET queued_at = updated_at WHERE state = 'queued' AND deleted_at IS NULL;

-- +goose Down
ALTER TABLE environment_work DROP COLUMN trace_parent;
ALTER TABLE environment_work DROP COLUMN queued_at;
