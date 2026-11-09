-- +goose Up
ALTER TABLE platform_oauth_providers
    DROP CONSTRAINT platform_oauth_providers_builtin_check,
    ADD CONSTRAINT platform_oauth_providers_builtin_check CHECK (
        (builtin_key IS NULL AND NOT protected AND external_id NOT IN ('poap_FeishuBuiltinOAuthClient', 'poap_FridayBuiltinOAuthClient') AND created_by_platform_admin_uuid IS NOT NULL)
        OR (builtin_key IS NOT NULL AND protected AND (
            (builtin_key = 'feishu' AND external_id = 'poap_FeishuBuiltinOAuthClient')
            OR (builtin_key = 'friday' AND external_id = 'poap_FridayBuiltinOAuthClient')
        ))
    ),
    DROP CONSTRAINT platform_oauth_providers_clients_check,
    ADD CONSTRAINT platform_oauth_providers_clients_check CHECK (
        (builtin_key IS NULL AND client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)
        OR (builtin_key IN ('feishu', 'friday') AND ((client_id_envelope IS NULL AND client_secret_envelope IS NULL) OR (client_id_envelope IS NOT NULL AND client_secret_envelope IS NOT NULL)))
    );
ALTER TABLE env_oauth_flows ADD COLUMN code_verifier_envelope jsonb;
ALTER TABLE env_oauth_flows
    DROP CONSTRAINT env_oauth_flows_client_snapshot_check,
    ADD CONSTRAINT env_oauth_flows_client_snapshot_check CHECK (
        (provider_key IN ('builtin:feishu', 'builtin:friday') AND client_id_envelope IS NULL)
        OR (provider_key NOT IN ('builtin:feishu', 'builtin:friday') AND client_id_envelope IS NOT NULL)
    );
INSERT INTO platform_oauth_providers (
    external_id, builtin_key, protected, name, authorize_endpoint, token_endpoint, scope,
    token_endpoint_auth_method, token_endpoint_encoding, default_secret_name, default_allowed_hosts
) VALUES (
    'poap_FridayBuiltinOAuthClient', 'friday', true, 'Friday',
    'https://friday-tx.seewo.com/open/api/oauth/authorize',
    'https://friday-tx.seewo.com/open/api/oauth/token', 'read',
    'client_secret_basic', 'form', '', '["friday-tx.seewo.com"]'
);

ALTER TABLE env_oauth_flows ADD CONSTRAINT env_oauth_flows_code_verifier_envelope_check CHECK (
    code_verifier_envelope IS NULL OR (
        jsonb_typeof(code_verifier_envelope) = 'object'
        AND code_verifier_envelope ?& ARRAY['ciphertext','nonce','wrapped_dek','format_version','key_provider','key_version']
        AND jsonb_typeof(code_verifier_envelope->'ciphertext') = 'string' AND length(code_verifier_envelope->>'ciphertext') > 0
        AND jsonb_typeof(code_verifier_envelope->'nonce') = 'string' AND length(code_verifier_envelope->>'nonce') > 0
        AND jsonb_typeof(code_verifier_envelope->'wrapped_dek') = 'string' AND length(code_verifier_envelope->>'wrapped_dek') > 0
        AND jsonb_typeof(code_verifier_envelope->'format_version') = 'number' AND code_verifier_envelope->>'format_version' = '1'
        AND jsonb_typeof(code_verifier_envelope->'key_provider') = 'string' AND length(code_verifier_envelope->>'key_provider') > 0
        AND jsonb_typeof(code_verifier_envelope->'key_version') = 'number' AND (code_verifier_envelope->>'key_version')::bigint > 0
    ) IS TRUE
);
ALTER TABLE env_oauth_flows ADD CONSTRAINT env_oauth_flows_pkce_check CHECK (
    (provider_key = 'builtin:friday' AND code_verifier_envelope IS NOT NULL)
    OR (provider_key <> 'builtin:friday' AND code_verifier_envelope IS NULL)
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'OAuth encrypted storage cannot be downgraded; restore the database and key backup'; END $$;
-- +goose StatementEnd
