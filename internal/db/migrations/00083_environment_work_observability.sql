-- +goose Up
ALTER TABLE environment_work ADD COLUMN IF NOT EXISTS queued_at timestamptz;
ALTER TABLE environment_work ADD COLUMN IF NOT EXISTS trace_parent text;
UPDATE environment_work SET queued_at = updated_at WHERE state = 'queued' AND deleted_at IS NULL;

-- +goose Down
ALTER TABLE environment_work DROP COLUMN IF EXISTS trace_parent;
ALTER TABLE environment_work DROP COLUMN IF EXISTS queued_at;
