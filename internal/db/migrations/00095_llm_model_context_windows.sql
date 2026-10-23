-- +goose Up
ALTER TABLE llm_providers ADD COLUMN model_context_windows jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE llm_providers DROP COLUMN model_context_windows;
