-- +goose Up
CREATE TABLE channel_reply_deliveries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL UNIQUE,
    external_id text NOT NULL UNIQUE,
    channel_uuid uuid NOT NULL,
    channel_external_id text NOT NULL,
    channel_type text NOT NULL,
    account_id text NOT NULL,
    input_event_id text NOT NULL,
    session_external_id text NOT NULL,
    scope_key text NOT NULL,
    message_id text NOT NULL,
    chat_id text NOT NULL,
    chat_type text NOT NULL,
    actor_id text NOT NULL,
    root_id text NOT NULL DEFAULT '',
    thread_id text NOT NULL DEFAULT '',
    reply_message_id text NOT NULL DEFAULT '',
    include_thinking boolean NOT NULL,
    include_tool_calls boolean NOT NULL,
    state text NOT NULL DEFAULT 'pending',
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (channel_uuid, input_event_id),
    CONSTRAINT channel_reply_delivery_state CHECK (state IN ('pending', 'sending', 'sent', 'completed', 'unconfirmed'))
);
CREATE INDEX channel_reply_deliveries_resume_idx ON channel_reply_deliveries(channel_external_id, lease_until) WHERE state IN ('sent', 'sending');
-- +goose Down
DROP TABLE channel_reply_deliveries;
