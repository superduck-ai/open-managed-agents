-- +goose Up
-- Keep the idle clock and deletion reason; use existing sandbox/work states.
DROP INDEX code_sessions_idle_reclaim_idx;
DROP INDEX code_sessions_runtime_wake_idx;
DROP INDEX session_events_runtime_pending_idx;
ALTER TABLE code_sessions DROP COLUMN runtime_state, DROP COLUMN runtime_wake_requested;
ALTER TABLE session_events DROP COLUMN runtime_pending;
CREATE INDEX code_sessions_idle_reclaim_idx ON code_sessions (idle_since, uuid)
WHERE status = 'active' AND worker_status = 'idle' AND deleted_at IS NULL;

-- +goose Down
-- Removed wake flags cannot be reconstructed; restore the original defaults.
DROP INDEX code_sessions_idle_reclaim_idx;
ALTER TABLE code_sessions
    ADD COLUMN runtime_state text NOT NULL DEFAULT 'resident'
        CHECK (runtime_state IN ('resident', 'reclaiming', 'reclaimed', 'waking')),
    ADD COLUMN runtime_wake_requested boolean NOT NULL DEFAULT false;
ALTER TABLE session_events ADD COLUMN runtime_pending boolean NOT NULL DEFAULT false;
CREATE INDEX code_sessions_idle_reclaim_idx ON code_sessions (idle_since, uuid)
WHERE status = 'active' AND runtime_state = 'resident' AND worker_status = 'idle' AND deleted_at IS NULL;
CREATE INDEX code_sessions_runtime_wake_idx ON code_sessions (uuid)
WHERE runtime_wake_requested AND deleted_at IS NULL;
CREATE INDEX session_events_runtime_pending_idx ON session_events (session_uuid, created_at, uuid)
WHERE runtime_pending AND deleted_at IS NULL;
