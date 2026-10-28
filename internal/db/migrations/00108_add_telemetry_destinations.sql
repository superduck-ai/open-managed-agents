-- +goose Up
CREATE TABLE IF NOT EXISTS telemetry_destinations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL,
    organization_uuid uuid NOT NULL,
    display_name text,
    endpoint_url text NOT NULL,
    traces_enabled boolean NOT NULL DEFAULT true,
    traces_endpoint_url text NOT NULL,
    traces_content_mode text NOT NULL DEFAULT 'metadata_only',
    traces_max_content_bytes integer NOT NULL,
    workspace_scope text NOT NULL DEFAULT 'all',
    header_names jsonb NOT NULL DEFAULT '[]'::jsonb,
    headers_ciphertext bytea,
    headers_nonce bytea,
    headers_wrapped_dek bytea,
    headers_format_version integer,
    headers_key_provider text,
    headers_key_version bigint,
    state text NOT NULL DEFAULT 'enabled',
    disabled_reason text,
    failure_started_at timestamptz,
    last_error text,
    last_error_at timestamptz,
    last_success_at timestamptz,
    last_test_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    CONSTRAINT telemetry_destinations_content_mode_check CHECK (traces_content_mode IN ('metadata_only', 'content')),
    CONSTRAINT telemetry_destinations_workspace_scope_check CHECK (workspace_scope IN ('all', 'include', 'exclude')),
    CONSTRAINT telemetry_destinations_state_check CHECK (state IN ('enabled', 'disabled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS telemetry_destinations_organization_external_id_key
    ON telemetry_destinations (organization_uuid, external_id);
CREATE INDEX IF NOT EXISTS telemetry_destinations_organization_active_idx
    ON telemetry_destinations (organization_uuid, created_at) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS telemetry_destination_workspaces (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    destination_uuid uuid NOT NULL,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS telemetry_destination_workspaces_key
    ON telemetry_destination_workspaces (destination_uuid, workspace_uuid);
CREATE INDEX IF NOT EXISTS telemetry_destination_workspaces_workspace_idx
    ON telemetry_destination_workspaces (organization_uuid, workspace_uuid);

-- +goose Down
DROP TABLE IF EXISTS telemetry_destination_workspaces;
DROP TABLE IF EXISTS telemetry_destinations;
