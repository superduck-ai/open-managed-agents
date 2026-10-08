-- +goose Up
CREATE TABLE event_payload_blobs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    bucket text NOT NULL,
    object_key text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes > 32768),
    sha256 text NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'attached', 'deleting')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX event_payload_blobs_cleanup_idx ON event_payload_blobs (updated_at, uuid);
ALTER TABLE session_events ADD COLUMN payload_blob_uuid uuid, ADD COLUMN tool_use_id text;
ALTER TABLE code_session_internal_events ADD COLUMN payload_blob_uuid uuid;
CREATE INDEX session_events_payload_blob_idx ON session_events (payload_blob_uuid) WHERE payload_blob_uuid IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX code_session_internal_events_payload_blob_idx ON code_session_internal_events (payload_blob_uuid) WHERE payload_blob_uuid IS NOT NULL AND deleted_at IS NULL;

-- +goose Down
-- Refuse to discard references while externalized history exists.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM session_events WHERE payload_blob_uuid IS NOT NULL)
       OR EXISTS (SELECT 1 FROM code_session_internal_events WHERE payload_blob_uuid IS NOT NULL) THEN
        RAISE EXCEPTION 'restore externalized event payloads before rollback';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE code_session_internal_events DROP COLUMN payload_blob_uuid;
ALTER TABLE session_events DROP COLUMN payload_blob_uuid, DROP COLUMN tool_use_id;
DROP TABLE event_payload_blobs;
