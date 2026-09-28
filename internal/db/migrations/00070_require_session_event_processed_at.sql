-- +goose Up
-- Resolve legacy queued inputs before requiring a processing time.
LOCK TABLE session_events IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM session_events WHERE processed_at IS NULL) THEN
        RAISE EXCEPTION 'resolve queued Session inputs before requiring processed_at';
    END IF;
    -- Some databases applied the former 00069 before it was removed from this PR.
    IF EXISTS (SELECT 1 FROM pg_attribute
               WHERE attrelid = 'session_events'::regclass AND attname = 'worker_ack_at' AND NOT attisdropped) THEN
        IF EXISTS (SELECT 1 FROM session_events
                   WHERE event_type IN ('user.custom_tool_result', 'user.tool_result')
                     AND worker_ack_at IS NULL AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'resolve unacknowledged Session tool results before removing worker ACK state';
        END IF;
        -- Equality may be the former 00069 backfill, not proof of delivery.
        IF EXISTS (SELECT 1 FROM session_events
                   WHERE event_type IN ('user.custom_tool_result', 'user.tool_result')
                     AND worker_ack_at = processed_at AND deleted_at IS NULL) THEN
            RAISE EXCEPTION 'audit backfilled Session tool result ACKs before removing worker ACK state';
        END IF;
        ALTER TABLE session_events DROP COLUMN worker_ack_at;
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE session_events ALTER COLUMN processed_at SET NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'cannot restore removed Worker ACK state';
END $$;
-- +goose StatementEnd
