-- +goose Up
CREATE TABLE harness_thread_state (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    thread_uuid uuid NOT NULL,
    processed_message_ids json NOT NULL DEFAULT '{}',
    metadata json,
    active_subscriptions json NOT NULL DEFAULT '{}',
    pending_user_choices json NOT NULL DEFAULT '[]',
    version bigint NOT NULL DEFAULT 0 CHECK (version >= 0),
    pending json NOT NULL DEFAULT '{}',
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, thread_uuid)
);

ALTER TABLE harness_thread_hierarchy DROP COLUMN state_data;

-- +goose Down
ALTER TABLE harness_thread_hierarchy ADD COLUMN state_data text NOT NULL DEFAULT '{}';
DROP TABLE harness_thread_state;
