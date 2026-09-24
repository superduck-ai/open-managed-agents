-- +goose NO TRANSACTION

-- +goose Up
-- A failed CONCURRENTLY build leaves an invalid index behind.
DROP INDEX CONCURRENTLY IF EXISTS session_events_processed_order_idx;
CREATE INDEX CONCURRENTLY session_events_processed_order_idx ON session_events (workspace_uuid, session_external_id, processed_at, id) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS session_events_processed_order_idx;
