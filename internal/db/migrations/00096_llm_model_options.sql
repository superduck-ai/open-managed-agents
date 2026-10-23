-- +goose Up
ALTER TABLE llm_providers ADD COLUMN model_options jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE llm_providers DROP COLUMN model_options;
