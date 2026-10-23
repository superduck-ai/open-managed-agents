-- +goose Up
CREATE TABLE usage_records (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL DEFAULT gen_random_uuid()::text,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    attempt_id text NOT NULL,
    source text NOT NULL CHECK (source IN ('managed_agents', 'api', 'workbench')),
    session_uuid uuid,
    session_external_id text NOT NULL DEFAULT '',
    api_key_uuid uuid,
    user_uuid uuid,
    model text NOT NULL,
    started_at timestamptz NOT NULL,
    ended_at timestamptz NOT NULL,
    input_tokens bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_read_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_write_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    cache_write_5m_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_write_5m_tokens >= 0),
    cache_write_1h_tokens bigint NOT NULL DEFAULT 0 CHECK (cache_write_1h_tokens >= 0),
    search_requests bigint NOT NULL DEFAULT 0 CHECK (search_requests >= 0),
    model_cents bigint CHECK (model_cents >= 0),
    search_cents bigint NOT NULL DEFAULT 0 CHECK (search_cents >= 0),
    complete boolean NOT NULL DEFAULT false,
    price_snapshot jsonb,
    version text NOT NULL DEFAULT 'request_cents_v1',
    finalized boolean NOT NULL DEFAULT true,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (workspace_uuid, source, attempt_id)
);
CREATE INDEX usage_records_scope_time ON usage_records (organization_uuid, workspace_uuid, ended_at);
CREATE INDEX usage_records_session_time ON usage_records (workspace_uuid, session_uuid, ended_at);

-- +goose Down
DROP TABLE usage_records;
