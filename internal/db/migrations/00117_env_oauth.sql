-- +goose Up
ALTER TABLE vault_credentials DROP CONSTRAINT IF EXISTS vault_credentials_auth_type_check;
ALTER TABLE vault_credentials ADD CONSTRAINT vault_credentials_auth_type_check
	CHECK (auth_type IN ('mcp_oauth', 'static_bearer', 'environment_variable', 'env_oauth'));

CREATE TABLE platform_oauth_providers (
	id bigint GENERATED ALWAYS AS IDENTITY,
	uuid uuid NOT NULL DEFAULT gen_random_uuid(),
	external_id text NOT NULL,
	name text NOT NULL,
	authorize_endpoint text NOT NULL,
	token_endpoint text NOT NULL,
	scope text NOT NULL DEFAULT '',
	client_id text NOT NULL,
	client_secret_envelope jsonb NOT NULL,
	token_endpoint_auth_method text NOT NULL DEFAULT 'client_secret_post',
	token_endpoint_encoding text NOT NULL DEFAULT 'form',
	created_by_platform_admin_uuid uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT platform_oauth_providers_pk PRIMARY KEY (id),
	CONSTRAINT platform_oauth_providers_uuid_key UNIQUE (uuid),
	CONSTRAINT platform_oauth_providers_external_id_key UNIQUE (external_id),
	CONSTRAINT platform_oauth_providers_name_key UNIQUE (name),
	CONSTRAINT platform_oauth_providers_token_endpoint_auth_method_check
		CHECK (token_endpoint_auth_method IN ('client_secret_post', 'client_secret_basic', 'none')),
	CONSTRAINT platform_oauth_providers_token_endpoint_encoding_check
		CHECK (token_endpoint_encoding IN ('form', 'json'))
);

CREATE INDEX platform_oauth_providers_created_v1_idx
	ON platform_oauth_providers (created_at DESC, id DESC);

CREATE TABLE env_oauth_flows (
	id bigint GENERATED ALWAYS AS IDENTITY,
	uuid uuid NOT NULL DEFAULT gen_random_uuid(),
	external_id text NOT NULL,
	organization_uuid uuid NOT NULL,
	workspace_uuid uuid NOT NULL,
	vault_uuid uuid NOT NULL,
	vault_external_id text NOT NULL,
	provider_key text NOT NULL,
	secret_name text NOT NULL,
	display_name text NOT NULL DEFAULT '',
	redirect_url text NOT NULL,
	authorize_endpoint text NOT NULL,
	token_endpoint text NOT NULL,
	client_id text NOT NULL,
	scope text NOT NULL DEFAULT '',
	token_endpoint_auth_method text NOT NULL,
	token_endpoint_encoding text NOT NULL,
	networking_type text NOT NULL,
	allowed_hosts jsonb NOT NULL DEFAULT '[]'::jsonb,
	status text NOT NULL,
	error_code text NOT NULL DEFAULT '',
	credential_external_id text NOT NULL DEFAULT '',
	created_by_api_key_uuid uuid,
	expires_at timestamptz NOT NULL,
	completed_at timestamptz,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	CONSTRAINT env_oauth_flows_pk PRIMARY KEY (id),
	CONSTRAINT env_oauth_flows_uuid_key UNIQUE (uuid),
	CONSTRAINT env_oauth_flows_external_id_key UNIQUE (external_id),
	CONSTRAINT env_oauth_flows_status_check CHECK (status IN ('pending', 'completed', 'failed', 'expired')),
	CONSTRAINT env_oauth_flows_networking_type_check CHECK (networking_type IN ('limited', 'unrestricted'))
);

CREATE INDEX env_oauth_flows_status_expires_v1_idx
	ON env_oauth_flows (status, expires_at);

-- +goose Down
DROP TABLE env_oauth_flows;
DROP TABLE platform_oauth_providers;
ALTER TABLE vault_credentials DROP CONSTRAINT IF EXISTS vault_credentials_auth_type_check;
ALTER TABLE vault_credentials ADD CONSTRAINT vault_credentials_auth_type_check
	CHECK (auth_type IN ('mcp_oauth', 'static_bearer', 'environment_variable'));
