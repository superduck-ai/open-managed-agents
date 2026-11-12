-- +goose Up
ALTER TABLE sessions ADD COLUMN last_event_seq bigint NOT NULL DEFAULT 0 CHECK (last_event_seq >= 0);
ALTER TABLE session_events
    ADD COLUMN seq bigint CHECK (seq > 0),
    ADD COLUMN queued_after_seq bigint CHECK (queued_after_seq >= 0);

UPDATE session_events AS events
SET seq = CASE WHEN events.processed_at IS NOT NULL THEN ordered.position END,
    queued_after_seq = CASE WHEN events.processed_at IS NULL THEN ordered.position END
FROM (
    SELECT uuid,
           SUM(CASE WHEN processed_at IS NOT NULL THEN 1 ELSE 0 END) OVER (
               PARTITION BY workspace_uuid, session_external_id
               ORDER BY COALESCE(processed_at, created_at), id
           ) AS position
    FROM session_events
) AS ordered
WHERE events.uuid = ordered.uuid;

UPDATE sessions
SET last_event_seq = latest.seq
FROM (
    SELECT workspace_uuid, session_external_id, MAX(seq) AS seq
    FROM session_events
    WHERE seq IS NOT NULL
    GROUP BY workspace_uuid, session_external_id
) AS latest
WHERE sessions.workspace_uuid = latest.workspace_uuid AND sessions.external_id = latest.session_external_id;

ALTER TABLE session_events
    ADD CONSTRAINT session_events_order_position CHECK ((processed_at IS NULL) = (seq IS NULL) AND (seq IS NOT NULL OR queued_after_seq IS NOT NULL));

CREATE UNIQUE INDEX session_events_seq_idx ON session_events (workspace_uuid, session_external_id, seq) WHERE seq IS NOT NULL;
CREATE INDEX session_events_order_idx ON session_events (workspace_uuid, session_external_id, (COALESCE(seq, queued_after_seq)), (seq IS NULL), id) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX session_events_order_idx;
DROP INDEX session_events_seq_idx;
ALTER TABLE session_events
    DROP CONSTRAINT session_events_order_position,
    DROP COLUMN queued_after_seq,
    DROP COLUMN seq;
ALTER TABLE sessions DROP COLUMN last_event_seq;
