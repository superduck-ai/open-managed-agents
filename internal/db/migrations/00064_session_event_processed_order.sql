-- +goose Up
CREATE INDEX IF NOT EXISTS session_events_processed_order_idx ON session_events (workspace_uuid, session_external_id, processed_at, external_id COLLATE "C") WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS session_events_processed_order_idx;
