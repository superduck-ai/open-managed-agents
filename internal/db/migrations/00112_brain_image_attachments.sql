-- +goose Up
CREATE TABLE brain_image_attachments (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    external_id text NOT NULL UNIQUE,
    organization_uuid uuid NOT NULL,
    workspace_uuid uuid NOT NULL,
    session_uuid uuid NOT NULL,
    sha256 text NOT NULL,
    media_type text NOT NULL,
    size_bytes bigint NOT NULL,
    bucket text NOT NULL,
    object_key text NOT NULL,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'attached', 'deleting')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    attached_at timestamptz,
    UNIQUE (workspace_uuid, session_uuid, sha256)
);
CREATE INDEX brain_image_attachments_cleanup ON brain_image_attachments (state, created_at);

-- +goose Down
DROP TABLE brain_image_attachments;
