-- +goose Up
ALTER TABLE platform_oauth_providers
    ADD COLUMN builtin_key text,
    ADD COLUMN protected boolean NOT NULL DEFAULT false,
    ADD COLUMN client_id_envelope jsonb,
    ADD COLUMN default_secret_name text NOT NULL DEFAULT '',
    ADD COLUMN default_allowed_hosts jsonb NOT NULL DEFAULT '[]',
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ALTER COLUMN client_secret_envelope DROP NOT NULL,
    ALTER COLUMN created_by_platform_admin_uuid DROP NOT NULL,
    DROP CONSTRAINT platform_oauth_providers_name_key,
    ADD CONSTRAINT platform_oauth_providers_builtin_check CHECK (
        (builtin_key IS NULL AND NOT protected AND external_id <> 'poap_FeishuBuiltinOAuthClient' AND created_by_platform_admin_uuid IS NOT NULL)
        OR (builtin_key IS NOT NULL AND builtin_key = 'feishu' AND protected AND external_id = 'poap_FeishuBuiltinOAuthClient')
    ),
    ADD CONSTRAINT platform_oauth_providers_builtin_key UNIQUE (builtin_key),
    ADD CONSTRAINT platform_oauth_providers_version_check CHECK (version > 0),
    ADD CONSTRAINT platform_oauth_providers_default_hosts_check CHECK (jsonb_typeof(default_allowed_hosts) = 'array');
CREATE UNIQUE INDEX platform_oauth_providers_custom_name_key ON platform_oauth_providers (name) WHERE builtin_key IS NULL;
ALTER TABLE env_oauth_flows ADD COLUMN client_id_envelope jsonb;
INSERT INTO platform_oauth_providers (
    external_id, builtin_key, protected, name, authorize_endpoint, token_endpoint, scope,
    client_id, token_endpoint_auth_method, token_endpoint_encoding, default_secret_name, default_allowed_hosts
) VALUES (
    'poap_FeishuBuiltinOAuthClient', 'feishu', true, 'Feishu',
    'https://accounts.feishu.cn/open-apis/authen/v1/authorize',
    'https://open.feishu.cn/open-apis/authen/v2/oauth/token', 'offline_access', '',
    'client_secret_post', 'json', 'LARKSUITE_CLI_USER_ACCESS_TOKEN', '["open.feishu.cn","accounts.feishu.cn"]'
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'OAuth encrypted storage cannot be downgraded; restore the database and key backup'; END $$;
-- +goose StatementEnd
