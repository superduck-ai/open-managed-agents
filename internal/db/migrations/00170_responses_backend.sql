-- +goose Up
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_api_backend_check;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_api_backend_check CHECK (api_backend IN ('messages', 'chat_completions', 'responses'));

-- +goose Down
ALTER TABLE llm_providers DROP CONSTRAINT llm_providers_api_backend_check;
ALTER TABLE llm_providers ADD CONSTRAINT llm_providers_api_backend_check CHECK (api_backend IN ('messages', 'chat_completions'));
