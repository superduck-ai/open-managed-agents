-- +goose Up
ALTER TABLE sandbox_tool_calls ADD COLUMN oauth_wait_started_at timestamptz;

-- +goose Down
ALTER TABLE sandbox_tool_calls DROP COLUMN oauth_wait_started_at;
