-- +goose Up
CREATE TABLE session_input_requests (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL DEFAULT ('sirq_' || replace(gen_random_uuid()::text, '-', '')) UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    request_key text NOT NULL,
    fingerprint text NOT NULL,
    event_ids jsonb NOT NULL,
    settled_input_event_ids jsonb NOT NULL,
    handoff_completed boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, request_key)
);
-- +goose Down
DROP TABLE session_input_requests;
