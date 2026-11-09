-- +goose Up
CREATE TABLE dsh_transcript_records (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    code_session_uuid uuid NOT NULL,
    transcript_uuid uuid NOT NULL,
    record_offset bigint NOT NULL CHECK (record_offset >= 0 AND record_offset <= 9007199254740992),
    payload bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (transcript_uuid, record_offset)
);
INSERT INTO dsh_transcript_records (organization_uuid, workspace_uuid, code_session_uuid, transcript_uuid, record_offset, payload, created_at)
SELECT organization_uuid, workspace_uuid, code_session_uuid, uuid, 0, convert_to(header::text, 'UTF8'), created_at FROM dsh_transcripts;
INSERT INTO dsh_transcript_records (uuid, organization_uuid, workspace_uuid, code_session_uuid, transcript_uuid, record_offset, payload, created_at)
SELECT uuid, organization_uuid, workspace_uuid, code_session_uuid, transcript_uuid, seq + 1, convert_to(payload::text, 'UTF8'), created_at FROM dsh_transcript_events;
ALTER TABLE dsh_transcripts DROP CONSTRAINT dsh_transcripts_next_seq_check;
ALTER TABLE dsh_transcripts RENAME COLUMN next_seq TO next_offset;
UPDATE dsh_transcripts SET next_offset = next_offset + 1;
ALTER TABLE dsh_transcripts ADD CHECK (next_offset >= 0 AND next_offset <= 9007199254740993);
ALTER TABLE dsh_transcripts DROP COLUMN header;
DROP TABLE dsh_transcript_events;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    RAISE EXCEPTION 'Transcript byte records cannot be downgraded to JSONB without losing confirmed byte identity';
END $$;
-- +goose StatementEnd
