-- +goose Up
ALTER TABLE environment_sandboxes ADD COLUMN tool_call_ready_at timestamptz;
CREATE TABLE sandbox_tool_calls (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    external_id text NOT NULL,
    thread_id text NOT NULL,
    worker_epoch bigint NOT NULL,
    sandbox_uuid uuid,
    attempt integer NOT NULL DEFAULT 0,
    tool_name text NOT NULL,
    input json,
    state text NOT NULL,
    deadline_at timestamptz NOT NULL,
    lease_expires_at timestamptz,
    cancel_requested_at timestamptz,
    cancel_reason text,
    stop_grace_until timestamptz,
    result jsonb,
    result_object_key text,
    result_bytes bigint,
    result_sha256 text,
    finalizer text,
    finalizer_state jsonb,
    finalize_timeout_ms bigint NOT NULL DEFAULT 30000,
    finalize_until timestamptz,
    error_code text,
    late_result jsonb,
    created_at timestamptz NOT NULL DEFAULT NOW(),
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    finished_at timestamptz,
    UNIQUE (code_session_uuid, thread_id, external_id),
    CHECK (state IN ('pending','dispatched','running','cancel_requested','finalizing','succeeded','failed','cancelled','expired','lost'))
);
CREATE INDEX sandbox_tool_calls_pending ON sandbox_tool_calls (code_session_uuid, id) WHERE state = 'pending';
CREATE INDEX sandbox_tool_calls_lease ON sandbox_tool_calls (lease_expires_at) WHERE state IN ('dispatched','running','cancel_requested');
CREATE INDEX sandbox_tool_calls_deadline ON sandbox_tool_calls (deadline_at) WHERE state = 'pending';
CREATE INDEX sandbox_tool_calls_finalizing ON sandbox_tool_calls (finalize_until) WHERE state = 'finalizing';

-- +goose StatementBegin
CREATE FUNCTION retire_sandbox_tool_calls() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.state IN ('stopping','stopped','failed') THEN
  UPDATE sandbox_tool_calls SET state='lost', finished_at=NOW(), updated_at=NOW()
  WHERE organization_uuid=NEW.organization_uuid AND workspace_uuid=NEW.workspace_uuid AND sandbox_uuid=NEW.uuid AND state IN ('dispatched','running','cancel_requested');
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER sandbox_tool_call_retirement AFTER UPDATE OF state ON environment_sandboxes FOR EACH ROW EXECUTE FUNCTION retire_sandbox_tool_calls();

-- +goose Down
DROP TRIGGER sandbox_tool_call_retirement ON environment_sandboxes;
DROP FUNCTION retire_sandbox_tool_calls();
DROP TABLE sandbox_tool_calls;
ALTER TABLE environment_sandboxes DROP COLUMN tool_call_ready_at;
