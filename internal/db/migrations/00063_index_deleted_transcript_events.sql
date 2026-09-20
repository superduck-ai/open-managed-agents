-- +goose Up
CREATE INDEX code_session_internal_events_deleted_session_idx
    ON code_session_internal_events (code_session_uuid, deleted_at)
    INCLUDE (organization_uuid, workspace_uuid, code_session_external_id, sequence_num)
    WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX code_session_internal_events_deleted_session_idx;
