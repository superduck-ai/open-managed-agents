-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS sessions_live_status_v1_idx ON sessions (status, updated_at) WHERE deleted_at IS NULL AND archived_at IS NULL AND status IN ('running', 'rescheduling');
CREATE INDEX CONCURRENTLY IF NOT EXISTS code_sessions_active_worker_state_v1_idx ON code_sessions (worker_status, connection_status) WHERE status = 'active' AND deleted_at IS NULL;
CREATE INDEX CONCURRENTLY IF NOT EXISTS environment_work_live_state_v1_idx ON environment_work (state) WHERE deleted_at IS NULL AND state IN ('queued', 'starting', 'active', 'stopping');
CREATE INDEX CONCURRENTLY IF NOT EXISTS environment_sandboxes_live_state_v1_idx ON environment_sandboxes (state) WHERE state IN ('creating', 'running', 'stopping');
DROP INDEX CONCURRENTLY IF EXISTS environment_work_live_state_v2_idx;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS environment_sandboxes_live_state_v1_idx;
DROP INDEX CONCURRENTLY IF EXISTS environment_work_live_state_v1_idx;
DROP INDEX CONCURRENTLY IF EXISTS code_sessions_active_worker_state_v1_idx;
DROP INDEX CONCURRENTLY IF EXISTS sessions_live_status_v1_idx;
