-- +goose Up
ALTER TABLE llm_providers ADD COLUMN api_backend text NOT NULL DEFAULT 'messages';
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_api_backend_check
    CHECK (api_backend IN ('messages', 'chat_completions'));

-- +goose Down
ALTER TABLE llm_providers DROP COLUMN api_backend;
