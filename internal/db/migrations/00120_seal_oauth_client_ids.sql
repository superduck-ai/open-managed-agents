-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE resource_id text;
BEGIN
    SELECT external_id INTO resource_id FROM platform_oauth_providers WHERE builtin_key IS NULL AND client_id_envelope IS NULL LIMIT 1;
    IF resource_id IS NOT NULL THEN
        RAISE EXCEPTION 'OAuth provider % requires offline oauth-provider-migrate --apply before startup', resource_id;
    END IF;
    SELECT external_id INTO resource_id FROM env_oauth_flows WHERE provider_key <> 'builtin:feishu' AND client_id_envelope IS NULL LIMIT 1;
    IF resource_id IS NOT NULL THEN
        RAISE EXCEPTION 'OAuth flow % requires offline oauth-provider-migrate --apply before startup', resource_id;
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE platform_oauth_providers
    ADD CONSTRAINT platform_oauth_providers_clients_check CHECK (
        (builtin_key IS NULL AND client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)
        OR (builtin_key IS NOT NULL AND builtin_key = 'feishu' AND ((client_id_envelope IS NULL AND client_secret_envelope IS NULL) OR (client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)))
    ),
    DROP COLUMN client_id;
ALTER TABLE env_oauth_flows
    DROP CONSTRAINT env_oauth_flows_builtin_client_id_check,
    ADD CONSTRAINT env_oauth_flows_client_snapshot_check CHECK (
        (provider_key = 'builtin:feishu' AND client_id_envelope IS NULL)
        OR (provider_key <> 'builtin:feishu' AND client_id_envelope IS NOT NULL)
    ),
    DROP COLUMN client_id;
ALTER TABLE platform_oauth_providers ADD CONSTRAINT platform_oauth_providers_client_id_envelope_check CHECK (
    client_id_envelope IS NULL OR (
        jsonb_typeof(client_id_envelope) = 'object'
        AND client_id_envelope ?& ARRAY['ciphertext','nonce','wrapped_dek','format_version','key_provider','key_version']
        AND jsonb_typeof(client_id_envelope->'ciphertext') = 'string' AND length(client_id_envelope->>'ciphertext') > 0
        AND jsonb_typeof(client_id_envelope->'nonce') = 'string' AND length(client_id_envelope->>'nonce') > 0
        AND jsonb_typeof(client_id_envelope->'wrapped_dek') = 'string' AND length(client_id_envelope->>'wrapped_dek') > 0
        AND jsonb_typeof(client_id_envelope->'format_version') = 'number' AND client_id_envelope->>'format_version' = '1'
        AND jsonb_typeof(client_id_envelope->'key_provider') = 'string' AND length(client_id_envelope->>'key_provider') > 0
        AND jsonb_typeof(client_id_envelope->'key_version') = 'number' AND (client_id_envelope->>'key_version')::bigint > 0
    ) IS TRUE
);
ALTER TABLE platform_oauth_providers ADD CONSTRAINT platform_oauth_providers_client_secret_envelope_check CHECK (
    client_secret_envelope IS NULL OR (
        jsonb_typeof(client_secret_envelope) = 'object'
        AND client_secret_envelope ?& ARRAY['ciphertext','nonce','wrapped_dek','format_version','key_provider','key_version']
        AND jsonb_typeof(client_secret_envelope->'ciphertext') = 'string' AND length(client_secret_envelope->>'ciphertext') > 0
        AND jsonb_typeof(client_secret_envelope->'nonce') = 'string' AND length(client_secret_envelope->>'nonce') > 0
        AND jsonb_typeof(client_secret_envelope->'wrapped_dek') = 'string' AND length(client_secret_envelope->>'wrapped_dek') > 0
        AND jsonb_typeof(client_secret_envelope->'format_version') = 'number' AND client_secret_envelope->>'format_version' = '1'
        AND jsonb_typeof(client_secret_envelope->'key_provider') = 'string' AND length(client_secret_envelope->>'key_provider') > 0
        AND jsonb_typeof(client_secret_envelope->'key_version') = 'number' AND (client_secret_envelope->>'key_version')::bigint > 0
    ) IS TRUE
);
ALTER TABLE env_oauth_flows ADD CONSTRAINT env_oauth_flows_client_id_envelope_check CHECK (
    client_id_envelope IS NULL OR (
        jsonb_typeof(client_id_envelope) = 'object'
        AND client_id_envelope ?& ARRAY['ciphertext','nonce','wrapped_dek','format_version','key_provider','key_version']
        AND jsonb_typeof(client_id_envelope->'ciphertext') = 'string' AND length(client_id_envelope->>'ciphertext') > 0
        AND jsonb_typeof(client_id_envelope->'nonce') = 'string' AND length(client_id_envelope->>'nonce') > 0
        AND jsonb_typeof(client_id_envelope->'wrapped_dek') = 'string' AND length(client_id_envelope->>'wrapped_dek') > 0
        AND jsonb_typeof(client_id_envelope->'format_version') = 'number' AND client_id_envelope->>'format_version' = '1'
        AND jsonb_typeof(client_id_envelope->'key_provider') = 'string' AND length(client_id_envelope->>'key_provider') > 0
        AND jsonb_typeof(client_id_envelope->'key_version') = 'number' AND (client_id_envelope->>'key_version')::bigint > 0
    ) IS TRUE
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'OAuth encrypted storage cannot be downgraded; restore the database and key backup'; END $$;
-- +goose StatementEnd
