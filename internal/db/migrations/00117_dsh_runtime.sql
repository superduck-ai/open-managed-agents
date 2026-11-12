-- +goose Up
CREATE TABLE dsh_transcripts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    dsh_session_id text NOT NULL,
    parent_dsh_session_id text,
    session_thread_id text,
    next_offset bigint NOT NULL DEFAULT 0 CHECK (next_offset >= 0 AND next_offset <= 9007199254740993),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, dsh_session_id)
);
CREATE TABLE dsh_transcript_records (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    transcript_uuid uuid NOT NULL,
    record_offset bigint NOT NULL CHECK (record_offset >= 0 AND record_offset <= 9007199254740992),
    payload bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (transcript_uuid, record_offset)
);
CREATE TABLE dsh_worker_states (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    context_snapshot jsonb CHECK (jsonb_typeof(context_snapshot) = 'object'),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid)
);
CREATE TABLE dsh_input_executions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    input_id text NOT NULL,
    session_thread_id text NOT NULL,
    status text NOT NULL DEFAULT 'accepted' CHECK (status IN ('accepted', 'running', 'completed')),
    result text CHECK (result IN ('success', 'cancelled', 'failed')),
    failure_code text CHECK (failure_code IN ('execution_failed', 'worker_shutdown', 'interrupted_recovery', 'quota_exhausted')),
    accepted_at timestamptz NOT NULL,
    started_at timestamptz,
    completed_at timestamptz,
    cancel_requested_at timestamptz,
    CHECK ((status = 'accepted' AND started_at IS NULL AND completed_at IS NULL AND result IS NULL)
        OR (status = 'running' AND started_at IS NOT NULL AND completed_at IS NULL AND result IS NULL)
        OR (status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND result IS NOT NULL)),
    CHECK ((result IS NOT DISTINCT FROM 'failed' AND failure_code IS NOT NULL) OR (result IS DISTINCT FROM 'failed' AND failure_code IS NULL)),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, input_id)
);
CREATE UNIQUE INDEX dsh_input_executions_running ON dsh_input_executions (code_session_uuid) WHERE status = 'running';
CREATE INDEX dsh_input_executions_active ON dsh_input_executions (organization_uuid, workspace_uuid, code_session_uuid, status, accepted_at, input_id) WHERE status IN ('accepted', 'running');
CREATE INDEX dsh_input_executions_completed ON dsh_input_executions (organization_uuid, workspace_uuid, code_session_uuid, accepted_at DESC, input_id DESC) WHERE status = 'completed';
CREATE TABLE dsh_permission_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    request_id text NOT NULL,
    input_id text,
    dsh_session_id text NOT NULL,
    session_thread_id text NOT NULL,
    tool_use_id text NOT NULL,
    turn text,
    tool jsonb,
    input jsonb,
    available boolean,
    worker_epoch bigint NOT NULL,
    public_event_id text NOT NULL,
    state text NOT NULL CHECK (state IN ('pending', 'resolved', 'cancelled', 'closed')),
    decision jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, request_id),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, dsh_session_id, tool_use_id)
);
CREATE INDEX dsh_permission_requests_pending ON dsh_permission_requests (organization_uuid, workspace_uuid, code_session_uuid, input_id) WHERE state = 'pending';
CREATE TABLE dsh_public_receipts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    event_id text NOT NULL,
    payload_digest text NOT NULL CHECK (payload_digest ~ '^exact-json-sha256-v1:[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, event_id)
);

-- +goose Down
DROP TABLE dsh_public_receipts;
DROP TABLE dsh_permission_requests;
DROP TABLE dsh_input_executions;
DROP TABLE dsh_worker_states;
DROP TABLE dsh_transcript_records;
DROP TABLE dsh_transcripts;
