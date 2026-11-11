-- +goose Up
UPDATE env_oauth_flows SET client_id = '' WHERE provider_key = 'builtin:feishu';
ALTER TABLE env_oauth_flows ADD CONSTRAINT env_oauth_flows_builtin_client_id_check
    CHECK (provider_key <> 'builtin:feishu' OR client_id = '');

-- +goose Down
ALTER TABLE env_oauth_flows DROP CONSTRAINT env_oauth_flows_builtin_client_id_check;
