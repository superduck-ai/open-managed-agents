-- +goose NO TRANSACTION

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS sessions_live_status_v1_idx;
CREATE INDEX CONCURRENTLY sessions_live_status_v1_idx ON sessions (status, updated_at) WHERE deleted_at IS NULL AND archived_at IS NULL AND status IN ('running', 'rescheduling');
DROP INDEX CONCURRENTLY IF EXISTS code_sessions_active_worker_state_v1_idx;
CREATE INDEX CONCURRENTLY code_sessions_active_worker_state_v1_idx ON code_sessions (worker_status, connection_status) WHERE status = 'active' AND deleted_at IS NULL;
DROP INDEX CONCURRENTLY IF EXISTS environment_work_live_state_v1_idx;
DROP INDEX CONCURRENTLY IF EXISTS environment_work_live_state_v2_idx;
CREATE INDEX CONCURRENTLY environment_work_live_state_v2_idx ON environment_work (state) WHERE deleted_at IS NULL AND state IN ('queued', 'starting', 'active', 'stopping');
DROP INDEX CONCURRENTLY IF EXISTS environment_sandboxes_live_state_v1_idx;
CREATE INDEX CONCURRENTLY environment_sandboxes_live_state_v1_idx ON environment_sandboxes (state) WHERE state IN ('creating', 'running', 'stopping');

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS environment_sandboxes_live_state_v1_idx;
DROP INDEX CONCURRENTLY IF EXISTS environment_work_live_state_v2_idx;
DROP INDEX CONCURRENTLY IF EXISTS code_sessions_active_worker_state_v1_idx;
DROP INDEX CONCURRENTLY IF EXISTS sessions_live_status_v1_idx;
