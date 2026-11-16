-- +goose Up
ALTER TABLE sandbox_tool_calls ADD COLUMN timeout_ms bigint;

-- +goose Down
ALTER TABLE sandbox_tool_calls DROP COLUMN timeout_ms;
