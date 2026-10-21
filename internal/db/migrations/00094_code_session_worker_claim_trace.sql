-- +goose Up
ALTER TABLE code_sessions ADD COLUMN IF NOT EXISTS worker_claim_trace_parent text;

-- +goose Down
ALTER TABLE code_sessions DROP COLUMN IF EXISTS worker_claim_trace_parent;
