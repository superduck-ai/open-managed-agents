-- +goose Up
UPDATE jobs AS j SET status = 'pending', run_after = NOW(), locked_by = NULL, locked_until = NULL, updated_at = NOW()
FROM dsh_input_deliveries AS d
WHERE j.uuid = d.cleanup_job_uuid AND j.workspace_uuid = d.workspace_uuid
    AND j.type = 'object_cleanup' AND j.status != 'completed';
DROP TABLE dsh_input_deliveries;

-- +goose Down
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
