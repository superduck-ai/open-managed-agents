-- +goose Up
ALTER TABLE code_sessions
	ADD COLUMN IF NOT EXISTS worker_claim_requested_at timestamptz,
	ADD COLUMN IF NOT EXISTS worker_claimed_by text,
	ADD COLUMN IF NOT EXISTS worker_claim_expires_at timestamptz,
	ADD COLUMN IF NOT EXISTS worker_claim_attempts integer NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS code_sessions_worker_claim_v1_idx
	ON code_sessions (worker_claim_requested_at)
	WHERE deleted_at IS NULL AND status = 'active'
		AND metadata #>> '{config,worker_transport}' = 'claim';

-- +goose Down
DROP INDEX IF EXISTS code_sessions_worker_claim_v1_idx;

ALTER TABLE code_sessions
	DROP COLUMN IF EXISTS worker_claim_attempts,
	DROP COLUMN IF EXISTS worker_claim_expires_at,
	DROP COLUMN IF EXISTS worker_claimed_by,
	DROP COLUMN IF EXISTS worker_claim_requested_at;
