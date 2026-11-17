-- +goose Up
CREATE TABLE channel_reply_deliveries_rebuilt (
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
    artifact_delivery jsonb NOT NULL DEFAULT '{}',
    state text NOT NULL DEFAULT 'pending',
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (channel_uuid, input_event_id),
    CONSTRAINT channel_reply_delivery_rebuilt_state CHECK (state IN ('pending', 'sending', 'sent', 'completed', 'unconfirmed'))
);

INSERT INTO channel_reply_deliveries_rebuilt (uuid, external_id, channel_uuid, channel_external_id, channel_type, account_id, input_event_id, session_external_id, scope_key, message_id, chat_id, chat_type, actor_id, root_id, thread_id, reply_message_id, include_thinking, include_tool_calls, state, lease_owner, lease_until, created_at, updated_at, artifact_delivery)
SELECT COALESCE((to_jsonb(d)->>'uuid')::uuid, gen_random_uuid()), COALESCE(to_jsonb(d)->>'external_id', 'crdl_' || replace(gen_random_uuid()::text, '-', '')), (SELECT c.uuid FROM channels c WHERE c.external_id=d.channel_external_id), d.channel_external_id, d.channel_type, d.account_id, d.input_event_id, d.session_external_id, d.scope_key, d.message_id, d.chat_id, d.chat_type, d.actor_id, d.root_id, d.thread_id, d.reply_message_id, d.include_thinking, d.include_tool_calls, d.state, d.lease_owner, d.lease_until, d.created_at, d.updated_at, d.artifact_delivery
FROM channel_reply_deliveries d;
DROP TABLE channel_reply_deliveries;
ALTER TABLE channel_reply_deliveries_rebuilt RENAME TO channel_reply_deliveries;
CREATE INDEX channel_reply_deliveries_resume_idx ON channel_reply_deliveries(channel_uuid, lease_until) WHERE state IN ('sent', 'sending');

-- +goose Down
SELECT 1;
