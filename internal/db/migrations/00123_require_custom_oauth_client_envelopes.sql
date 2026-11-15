-- +goose Up
ALTER TABLE platform_oauth_providers
    DROP CONSTRAINT platform_oauth_providers_clients_check,
    ADD CONSTRAINT platform_oauth_providers_clients_check CHECK (
        (builtin_key IS NULL AND client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)
        OR (builtin_key IS NOT NULL AND builtin_key IN ('feishu', 'friday') AND ((client_id_envelope IS NULL AND client_secret_envelope IS NULL) OR (client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)))
    );

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'OAuth encrypted storage cannot be downgraded; restore the database and key backup'; END $$;
-- +goose StatementEnd
