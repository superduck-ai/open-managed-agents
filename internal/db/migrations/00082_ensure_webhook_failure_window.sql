-- +goose Up
ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS failure_started_at timestamptz;

UPDATE jobs
SET status = 'failed', locked_by = NULL, locked_until = NULL, updated_at = NOW(),
    payload = payload || jsonb_build_object('last_error', 'webhook delivery queue retired; task not migrated to JetStream')
WHERE type = 'webhook_delivery' AND status IN ('pending', 'retry', 'running');

-- +goose Down
SELECT 1;
