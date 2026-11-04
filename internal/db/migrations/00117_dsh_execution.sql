-- +goose Up
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
CREATE TABLE dsh_input_deliveries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    input_execution_uuid uuid NOT NULL,
    input_id text NOT NULL,
    ack_subject text NOT NULL CHECK (ack_subject != ''),
    cleanup_job_uuid uuid,
    status text NOT NULL CHECK (status IN ('waiting', 'pending', 'discarded')),
    delivered_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_until timestamptz,
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (lease_token IS NULL OR status = 'pending'),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, input_id)
);
CREATE INDEX dsh_input_deliveries_pending ON dsh_input_deliveries (next_attempt_at, uuid) WHERE status = 'pending';
CREATE INDEX dsh_input_deliveries_discarded ON dsh_input_deliveries (delivered_at, uuid) WHERE status = 'discarded';
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
    payload_digest text NOT NULL CHECK (payload_digest ~ '^jcs-sha256-v1:[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, event_id)
);
-- +goose Down
DROP TABLE dsh_public_receipts;
DROP TABLE dsh_permission_requests;
DROP TABLE dsh_input_deliveries;
DROP TABLE dsh_input_executions;
