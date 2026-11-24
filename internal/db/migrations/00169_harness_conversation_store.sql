-- +goose Up
CREATE TABLE harness_thread_hierarchy (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    parent_thread_uuid uuid,
    root_thread_uuid uuid NOT NULL,
    spawn_message_order bigint,
    spawn_tool_call_id text,
    closed boolean NOT NULL DEFAULT false,
    is_subscription_event boolean NOT NULL DEFAULT false,
    is_compaction boolean NOT NULL DEFAULT false,
    fresh_context boolean NOT NULL DEFAULT false,
    message_count bigint NOT NULL DEFAULT 0 CHECK (message_count >= 0),
    state_data text NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX harness_thread_session_idx ON harness_thread_hierarchy (organization_uuid, workspace_uuid, session_uuid);

CREATE TABLE harness_conversation_history (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    thread_uuid uuid NOT NULL,
    turn_uuid uuid,
    message_order bigint NOT NULL CHECK (message_order > 0),
    message_id text NOT NULL,
    message_data text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, thread_uuid, message_order),
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, thread_uuid, message_id)
);

CREATE INDEX harness_history_turn_idx ON harness_conversation_history (organization_uuid, workspace_uuid, session_uuid, thread_uuid, turn_uuid)
    WHERE turn_uuid IS NOT NULL;

CREATE TABLE harness_compaction_summaries (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    thread_uuid uuid NOT NULL,
    up_to_order bigint NOT NULL CHECK (up_to_order >= 0),
    summary text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, session_uuid, thread_uuid, up_to_order)
);

-- +goose Down
DROP TABLE harness_compaction_summaries;
DROP TABLE harness_conversation_history;
DROP TABLE harness_thread_hierarchy;
