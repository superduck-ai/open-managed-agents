-- +goose NO TRANSACTION

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS session_events_first_user_message_idx;
CREATE INDEX CONCURRENTLY session_events_first_user_message_idx
    ON session_events (workspace_uuid, session_external_id, thread_uuid, id)
    WHERE event_type = 'user.message' AND deleted_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS session_events_first_user_message_idx;
