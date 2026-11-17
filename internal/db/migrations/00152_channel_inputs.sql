-- +goose Up
CREATE TABLE channel_inputs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL DEFAULT ('cinp_' || replace(gen_random_uuid()::text, '-', '')) UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    channel_uuid uuid NOT NULL,
    channel_type text NOT NULL,
    account_id text NOT NULL,
    message_id text NOT NULL,
    scope_key text NOT NULL,
    payload jsonb NOT NULL,
    parent_uuid uuid,
    state text NOT NULL DEFAULT 'received',
    session_external_id text NOT NULL DEFAULT '',
    prepared_content jsonb,
    input_event_id text NOT NULL DEFAULT '',
    lease_owner text NOT NULL DEFAULT '',
    lease_until timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(channel_uuid, channel_type, account_id, message_id),
    CONSTRAINT channel_input_state CHECK(state IN ('received','buffering','prepared','accepted','grouped','handed_off','command_sending'))
);
CREATE INDEX channel_inputs_recovery_idx ON channel_inputs(channel_uuid, channel_type, account_id, lease_until)
WHERE state IN ('received','buffering','prepared','accepted');
-- +goose Down
DROP TABLE channel_inputs;
