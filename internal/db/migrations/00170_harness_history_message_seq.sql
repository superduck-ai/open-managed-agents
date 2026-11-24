-- +goose Up
ALTER TABLE harness_conversation_history
    DROP COLUMN uuid,
    DROP COLUMN external_id;

ALTER TABLE harness_conversation_history
    RENAME COLUMN message_order TO message_seq;

ALTER TABLE harness_conversation_history
    RENAME CONSTRAINT harness_conversation_history_message_order_check TO harness_conversation_history_message_seq_check;

-- +goose Down
ALTER TABLE harness_conversation_history
    RENAME CONSTRAINT harness_conversation_history_message_seq_check TO harness_conversation_history_message_order_check;

ALTER TABLE harness_conversation_history
    RENAME COLUMN message_seq TO message_order;

ALTER TABLE harness_conversation_history
    ADD COLUMN uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    ADD COLUMN external_id text NOT NULL DEFAULT ('hmsg_' || gen_random_uuid());

ALTER TABLE harness_conversation_history
    ALTER COLUMN external_id DROP DEFAULT;
