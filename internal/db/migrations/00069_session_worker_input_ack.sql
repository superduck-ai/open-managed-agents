-- +goose Up
ALTER TABLE session_events ADD COLUMN worker_ack_at timestamptz;

-- Previously processed tool results have already passed the old ACK gate.
UPDATE session_events
SET worker_ack_at = processed_at
WHERE event_type IN ('user.custom_tool_result', 'user.tool_result')
  AND processed_at IS NOT NULL;

-- +goose Down
-- Older code cannot distinguish an immediately processed, undelivered tool result.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM session_events
        WHERE event_type IN ('user.custom_tool_result', 'user.tool_result')
          AND processed_at IS NOT NULL AND worker_ack_at IS NULL
    ) THEN
        RAISE EXCEPTION 'acknowledge pending tool results before rollback';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE session_events DROP COLUMN worker_ack_at;
