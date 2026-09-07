-- +goose Up
ALTER TABLE sessions ADD COLUMN last_event_at timestamptz NOT NULL DEFAULT '1970-01-01T00:00:00Z';

UPDATE sessions s SET last_event_at = e.last_event_at
FROM (
    SELECT workspace_uuid, session_uuid, MAX(GREATEST(created_at, processed_at)) AS last_event_at
    FROM session_events GROUP BY workspace_uuid, session_uuid
) e
WHERE s.workspace_uuid = e.workspace_uuid AND s.uuid = e.session_uuid;

CREATE INDEX session_events_processed_order_idx
    ON session_events (workspace_uuid, session_external_id, processed_at, external_id COLLATE "C");
CREATE INDEX session_events_thread_processed_order_idx
    ON session_events (workspace_uuid, session_external_id, thread_external_id, processed_at, external_id COLLATE "C");

-- +goose Down
DROP INDEX session_events_thread_processed_order_idx;
DROP INDEX session_events_processed_order_idx;
ALTER TABLE sessions DROP COLUMN last_event_at;
