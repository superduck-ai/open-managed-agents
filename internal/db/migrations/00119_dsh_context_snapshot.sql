-- +goose Up
ALTER TABLE dsh_worker_states ADD COLUMN context_snapshot jsonb CHECK (jsonb_typeof(context_snapshot) = 'object');
-- +goose Down
ALTER TABLE dsh_worker_states DROP COLUMN context_snapshot;
