-- +goose Up
CREATE TABLE dsh_transcripts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    dsh_session_id text NOT NULL,
    parent_dsh_session_id text,
    session_thread_id text,
    header jsonb NOT NULL,
    next_seq bigint NOT NULL DEFAULT 0 CHECK (next_seq >= 0 AND next_seq <= 9007199254740991),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, code_session_uuid, dsh_session_id)
);
CREATE TABLE dsh_transcript_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    transcript_uuid uuid NOT NULL,
    seq bigint NOT NULL CHECK (seq >= 0 AND seq < 9007199254740991),
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (organization_uuid, workspace_uuid, transcript_uuid, seq)
);
CREATE INDEX dsh_transcript_events_scope ON dsh_transcript_events (organization_uuid, workspace_uuid, code_session_uuid, transcript_uuid, seq);

-- +goose Down
DROP TABLE dsh_transcript_events;
DROP TABLE dsh_transcripts;
