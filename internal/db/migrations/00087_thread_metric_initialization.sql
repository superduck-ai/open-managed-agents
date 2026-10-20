-- +goose Up
ALTER TABLE session_threads ADD COLUMN stats_initialized boolean NOT NULL DEFAULT false;
ALTER TABLE session_threads ADD COLUMN usage_initialized boolean NOT NULL DEFAULT false;
UPDATE session_threads AS thread
SET stats_initialized = EXISTS (
    SELECT 1 FROM session_events AS event
    WHERE event.workspace_uuid = thread.workspace_uuid AND event.session_uuid = thread.session_uuid
      AND event.event_type IN ('session.thread_status_running', 'session.thread_status_idle', 'session.thread_status_rescheduled', 'session.thread_status_terminated')
      AND event.payload->>'session_thread_id' = thread.external_id
),
usage_initialized = EXISTS (
    SELECT 1 FROM session_events AS event
    WHERE event.workspace_uuid = thread.workspace_uuid AND event.session_uuid = thread.session_uuid
      AND event.event_type = 'session.thread_status_idle'
      AND event.payload->>'session_thread_id' = thread.external_id
);

-- +goose Down
ALTER TABLE session_threads DROP COLUMN usage_initialized;
ALTER TABLE session_threads DROP COLUMN stats_initialized;
