-- +goose Up
CREATE SEQUENCE session_event_delivery_seq;
ALTER TABLE session_events ADD COLUMN delivery_seq bigint;
UPDATE session_events SET delivery_seq = id WHERE processed_at IS NOT NULL;
SELECT setval('session_event_delivery_seq', COALESCE(MAX(id), 1), COUNT(*) > 0) FROM session_events;
CREATE INDEX session_events_delivery_idx ON session_events (workspace_uuid, session_external_id, delivery_seq)
    WHERE delivery_seq IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
DROP INDEX session_events_delivery_idx;
ALTER TABLE session_events DROP COLUMN delivery_seq;
DROP SEQUENCE session_event_delivery_seq;
