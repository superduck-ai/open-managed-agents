-- +goose Up
CREATE TABLE runtime_profiles (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid                   uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id            text NOT NULL UNIQUE,
    scope                  text NOT NULL CHECK (scope IN ('platform', 'organization')),
    organization_uuid      uuid REFERENCES organizations(uuid),
    name                   text NOT NULL,
    provider_kind          text NOT NULL,
    endpoint_url           text NOT NULL DEFAULT '',
    isolation_mode         text NOT NULL DEFAULT 'shared_upstream'
        CHECK (isolation_mode IN ('isolated', 'shared_upstream')),
    config                 jsonb NOT NULL DEFAULT '{}'::jsonb,
    capability_overrides   jsonb NOT NULL DEFAULT '{}'::jsonb,
    secret_envelope        jsonb,
    credential_hint        text NOT NULL DEFAULT '',
    credential_revision    bigint NOT NULL DEFAULT 1,
    status                 text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'archived')),
    last_health_status     text NOT NULL DEFAULT 'unknown',
    last_health_checked_at timestamptz,
    last_health_error      text NOT NULL DEFAULT '',
    version                bigint NOT NULL DEFAULT 1,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT runtime_profiles_scope_org_check CHECK (
        (scope = 'platform' AND organization_uuid IS NULL) OR
        (scope = 'organization' AND organization_uuid IS NOT NULL)
    )
);

CREATE UNIQUE INDEX runtime_profiles_platform_name_idx
    ON runtime_profiles (name) WHERE scope = 'platform';
CREATE UNIQUE INDEX runtime_profiles_org_name_idx
    ON runtime_profiles (organization_uuid, name) WHERE scope = 'organization';

INSERT INTO runtime_profiles (uuid, external_id, scope, name, provider_kind, isolation_mode, status)
VALUES (
    '00000000-0000-4000-8000-000000000001',
    'rprof_oma_native_default',
    'platform',
    'OMA Native',
    'oma_native',
    'isolated',
    'active'
) ON CONFLICT (uuid) DO NOTHING;

ALTER TABLE organizations
    ADD COLUMN default_runtime_profile_uuid uuid NOT NULL
        DEFAULT '00000000-0000-4000-8000-000000000001'
        REFERENCES runtime_profiles(uuid),
    ADD COLUMN allowed_runtime_profile_uuids uuid[] NOT NULL
        DEFAULT ARRAY['00000000-0000-4000-8000-000000000001'::uuid];

ALTER TABLE workspaces
    ADD COLUMN runtime_profile_uuid uuid NOT NULL
        DEFAULT '00000000-0000-4000-8000-000000000001'
        REFERENCES runtime_profiles(uuid);

CREATE TABLE runtime_resource_bindings (
    id                     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_uuid      uuid NOT NULL,
    workspace_uuid         uuid NOT NULL,
    runtime_profile_uuid   uuid NOT NULL REFERENCES runtime_profiles(uuid),
    provider_kind          text NOT NULL,
    resource_type          text NOT NULL CHECK (resource_type IN (
        'agent', 'environment', 'session', 'file', 'skill', 'vault', 'memory_store'
    )),
    oma_external_id        text NOT NULL,
    remote_id              text NOT NULL,
    parent_oma_external_id text NOT NULL DEFAULT '',
    resource_role          text NOT NULL DEFAULT '',
    state                  text NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived', 'deleted')),
    summary                jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, resource_type, oma_external_id),
    UNIQUE (runtime_profile_uuid, resource_type, remote_id)
);

CREATE INDEX runtime_resource_bindings_ws_type_idx
    ON runtime_resource_bindings (organization_uuid, workspace_uuid, resource_type, state, created_at DESC);

CREATE TABLE provider_usage_records (
    id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_uuid    uuid NOT NULL,
    workspace_uuid       uuid NOT NULL,
    runtime_profile_uuid uuid NOT NULL REFERENCES runtime_profiles(uuid),
    provider_kind        text NOT NULL,
    session_external_id  text NOT NULL,
    remote_session_id    text NOT NULL,
    metric_unit          text NOT NULL CHECK (metric_unit IN ('credits', 'usd_cents', 'tokens')),
    cumulative_amount    numeric(20,6) NOT NULL DEFAULT 0,
    delta_amount         numeric(20,6) NOT NULL DEFAULT 0,
    raw_usage            jsonb NOT NULL DEFAULT '{}'::jsonb,
    sampled_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_uuid, session_external_id, metric_unit, sampled_at)
);

CREATE INDEX provider_usage_scope_time_idx
    ON provider_usage_records (organization_uuid, workspace_uuid, sampled_at DESC);

-- +goose Down
DROP INDEX IF EXISTS provider_usage_scope_time_idx;
DROP TABLE IF EXISTS provider_usage_records;
DROP INDEX IF EXISTS runtime_resource_bindings_ws_type_idx;
DROP TABLE IF EXISTS runtime_resource_bindings;
ALTER TABLE workspaces DROP COLUMN IF EXISTS runtime_profile_uuid;
ALTER TABLE organizations
    DROP COLUMN IF EXISTS allowed_runtime_profile_uuids,
    DROP COLUMN IF EXISTS default_runtime_profile_uuid;
DROP INDEX IF EXISTS runtime_profiles_org_name_idx;
DROP INDEX IF EXISTS runtime_profiles_platform_name_idx;
DROP TABLE IF EXISTS runtime_profiles;
