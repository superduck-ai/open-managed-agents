-- +goose Up
DROP TABLE IF EXISTS event_outbox;
DROP TABLE IF EXISTS code_session_inbound_events;

ALTER TABLE code_sessions
    DROP COLUMN IF EXISTS last_inbound_sequence_num;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    RAISE EXCEPTION 'migration 00059 is intentionally irreversible because legacy inbound events are not restored';
END
$$;
-- +goose StatementEnd
