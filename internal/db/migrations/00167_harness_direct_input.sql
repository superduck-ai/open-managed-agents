-- +goose Up
UPDATE session_input_requests r
SET handoff_completed = false
WHERE r.handoff_completed
  AND EXISTS (
      SELECT 1 FROM cloud_harness_turns turn
      JOIN session_events input
        ON input.uuid = turn.input_event_uuid AND input.organization_uuid = turn.organization_uuid
       AND input.workspace_uuid = turn.workspace_uuid AND input.session_uuid = turn.session_uuid
      WHERE turn.organization_uuid = r.organization_uuid AND turn.workspace_uuid = r.workspace_uuid
        AND turn.session_uuid = r.session_uuid AND turn.state = 'pending' AND turn.published_at IS NULL
        AND input.deleted_at IS NULL AND input.processed_at IS NULL AND r.event_ids ? input.external_id
  );

DROP INDEX cloud_harness_turns_unpublished_idx;
DROP INDEX cloud_harness_turns_unfinished_idx;
ALTER TABLE cloud_harness_turns DROP COLUMN published_at;

-- +goose Down
ALTER TABLE cloud_harness_turns ADD COLUMN published_at timestamptz;
CREATE INDEX cloud_harness_turns_unfinished_idx
    ON cloud_harness_turns (organization_uuid, workspace_uuid, session_uuid, id)
    WHERE state IN ('pending', 'running');
CREATE INDEX cloud_harness_turns_unpublished_idx
    ON cloud_harness_turns (id) WHERE state = 'pending' AND published_at IS NULL;
