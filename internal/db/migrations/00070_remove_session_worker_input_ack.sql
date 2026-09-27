-- +goose Up
-- Drain or explicitly resolve legacy queued inputs and unacknowledged tool results
-- with the old worker delivery path before switching application versions.
LOCK TABLE session_events IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM session_events WHERE processed_at IS NULL) THEN
        RAISE EXCEPTION 'resolve queued Session inputs before removing worker ACK state';
    END IF;
    IF EXISTS (
        SELECT 1 FROM session_events
        WHERE event_type IN ('user.custom_tool_result', 'user.tool_result')
          AND worker_ack_at IS NULL AND deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'resolve unacknowledged Session tool results before removing worker ACK state';
    END IF;
    -- 00069 copied processed_at into worker_ack_at for older tool results.
    -- Equality cannot prove delivery; these rows need an external audit.
    IF EXISTS (
        SELECT 1 FROM session_events
        WHERE event_type IN ('user.custom_tool_result', 'user.tool_result')
          AND worker_ack_at = processed_at AND deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'audit backfilled Session tool result ACKs before removing worker ACK state';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE session_events ALTER COLUMN processed_at SET NOT NULL;
ALTER TABLE session_events DROP COLUMN worker_ack_at;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM session_events
        WHERE event_type IN ('user.custom_tool_result', 'user.tool_result') AND deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot reconstruct Worker ACK state for Session tool results';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE session_events ADD COLUMN worker_ack_at timestamptz;
ALTER TABLE session_events ALTER COLUMN processed_at DROP NOT NULL;
