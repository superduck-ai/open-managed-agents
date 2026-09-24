-- +goose Up
ALTER TABLE webhook_endpoints ADD COLUMN failure_started_at timestamptz;

-- +goose Down
ALTER TABLE webhook_endpoints DROP COLUMN failure_started_at;
