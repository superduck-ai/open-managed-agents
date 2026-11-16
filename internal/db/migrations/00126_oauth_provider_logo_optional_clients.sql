-- +goose Up
ALTER TABLE platform_oauth_providers
    ADD COLUMN logo_url text NOT NULL DEFAULT '',
    DROP CONSTRAINT platform_oauth_providers_clients_check,
    ADD CONSTRAINT platform_oauth_providers_clients_check CHECK (
        (client_id_envelope IS NULL) = (client_secret_envelope IS NULL)
    );

-- +goose Down
ALTER TABLE platform_oauth_providers
    DROP COLUMN logo_url,
    DROP CONSTRAINT platform_oauth_providers_clients_check,
    ADD CONSTRAINT platform_oauth_providers_clients_check CHECK (
        (builtin_key IS NULL AND client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)
        OR (builtin_key IS NOT NULL AND builtin_key = 'feishu' AND ((client_id_envelope IS NULL AND client_secret_envelope IS NULL) OR (client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)))
    );
