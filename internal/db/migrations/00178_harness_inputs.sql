-- +goose Up
CREATE TABLE harness_inputs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    input_uuid uuid NOT NULL UNIQUE,
    session_uuid uuid NOT NULL,
    input_message jsonb NOT NULL CHECK (jsonb_typeof(input_message) = 'object'),
    state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'claimed', 'cancelled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);

CREATE INDEX harness_inputs_queue_idx ON harness_inputs (session_uuid, id) WHERE state = 'queued';
CREATE INDEX harness_inputs_unpublished_idx ON harness_inputs (id) WHERE state = 'queued' AND published_at IS NULL;

INSERT INTO harness_inputs (input_uuid, session_uuid, input_message, state, created_at)
SELECT e.uuid, e.session_uuid,
    jsonb_build_object('legacy_event', e.payload, 'payload_blob_uuid', e.payload_blob_uuid),
    CASE WHEN t.execution_input_uuid IS NULL THEN 'queued' ELSE 'claimed' END,
    e.created_at
FROM session_events e
JOIN sessions s ON s.uuid = e.session_uuid AND s.workspace_uuid = e.workspace_uuid
    AND s.organization_uuid = e.organization_uuid
LEFT JOIN harness_thread_state t ON t.thread_uuid = s.uuid AND t.session_uuid = s.uuid
    AND t.workspace_uuid = s.workspace_uuid AND t.organization_uuid = s.organization_uuid
    AND t.execution_input_uuid = e.uuid
    AND t.execution->>'state' IN ('pending', 'running', 'waiting_choice', 'waiting_tool')
WHERE s.execution_runtime = 'cloud_harness' AND s.deleted_at IS NULL
    AND s.archived_at IS NULL AND s.status <> 'terminated'
    AND e.event_type = 'user.message' AND e.deleted_at IS NULL
    AND (e.processed_at IS NULL OR t.execution_input_uuid IS NOT NULL)
ORDER BY e.id;

-- +goose Down
DROP TABLE harness_inputs;
