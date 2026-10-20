-- +goose Up
ALTER TABLE session_resources ADD COLUMN source_file_uuid uuid;

CREATE INDEX session_resources_active_source_file_idx
    ON session_resources (workspace_uuid, source_file_uuid)
    WHERE source_file_uuid IS NOT NULL AND deleted_at IS NULL;

CREATE UNIQUE INDEX session_resources_input_file_idx
    ON session_resources (file_uuid) WHERE source_file_uuid IS NOT NULL;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM session_resources resource
        WHERE resource.resource_type = 'file' AND resource.payload IS NOT NULL
            AND resource.deleted_at IS NULL
            AND NOT EXISTS (
                SELECT 1 FROM files source WHERE source.uuid = resource.file_uuid
                    AND source.workspace_uuid = resource.workspace_uuid AND source.deleted_at IS NULL
            )
    ) THEN
        RAISE EXCEPTION 'active session input has no source file in its workspace';
    END IF;
END $$;
-- +goose StatementEnd

CREATE TEMP TABLE session_input_file_backfill ON COMMIT DROP AS
SELECT resource.uuid AS resource_uuid, resource.file_uuid AS source_file_uuid,
    gen_random_uuid() AS instance_uuid,
    concat('file_', replace(CAST(gen_random_uuid() AS text), '-', '')) AS instance_id,
    resource.workspace_uuid, resource.session_uuid, resource.session_external_id,
    resource.created_at
FROM session_resources resource
JOIN files source ON source.uuid = resource.file_uuid
    AND source.workspace_uuid = resource.workspace_uuid AND source.deleted_at IS NULL
WHERE resource.resource_type = 'file' AND resource.payload IS NOT NULL
    AND resource.deleted_at IS NULL AND resource.file_uuid IS NOT NULL;

INSERT INTO files (
    uuid, external_id, workspace_uuid, filename, mime_type, size_bytes, sha256,
    s3_bucket, s3_key, downloadable, scope_type, scope_id, created_by_api_key_uuid,
    created_at, detected_mime_type, metadata, authorization_metadata, tags,
    md5, s3_etag, s3_version_id
)
SELECT mapping.instance_uuid, mapping.instance_id, source.workspace_uuid,
    source.filename, source.mime_type, source.size_bytes, source.sha256,
    source.s3_bucket, source.s3_key, source.downloadable, 'session', mapping.session_external_id,
    source.created_by_api_key_uuid, mapping.created_at, source.detected_mime_type,
    source.metadata, source.authorization_metadata, source.tags, source.md5,
    source.s3_etag, source.s3_version_id
FROM session_input_file_backfill mapping
JOIN files source ON source.uuid = mapping.source_file_uuid
    AND source.workspace_uuid = mapping.workspace_uuid;

UPDATE session_resources resource
SET file_uuid = mapping.instance_uuid, source_file_uuid = mapping.source_file_uuid,
    payload = jsonb_set(resource.payload, '{file_id}', to_jsonb(mapping.instance_id))
FROM session_input_file_backfill mapping
WHERE resource.uuid = mapping.resource_uuid AND resource.workspace_uuid = mapping.workspace_uuid;

ALTER TABLE session_resources ADD CONSTRAINT session_resources_input_source_check
    CHECK (source_file_uuid IS NULL OR (
        resource_type = 'file' AND payload IS NOT NULL AND file_uuid IS NOT NULL
        AND source_file_uuid <> file_uuid
    ));

-- +goose Down
CREATE TEMP TABLE session_input_file_rollback ON COMMIT DROP AS
SELECT uuid AS resource_uuid, workspace_uuid, file_uuid, source_file_uuid
FROM session_resources WHERE source_file_uuid IS NOT NULL;

UPDATE session_resources resource
SET file_uuid = mapping.source_file_uuid, source_file_uuid = NULL,
    payload = jsonb_set(resource.payload, '{file_id}', to_jsonb(source.external_id))
FROM session_input_file_rollback mapping
JOIN files source ON source.uuid = mapping.source_file_uuid
    AND source.workspace_uuid = mapping.workspace_uuid
WHERE resource.uuid = mapping.resource_uuid AND resource.workspace_uuid = mapping.workspace_uuid;

DELETE FROM files instance USING session_input_file_rollback mapping
WHERE instance.uuid = mapping.file_uuid AND instance.workspace_uuid = mapping.workspace_uuid;

ALTER TABLE session_resources DROP COLUMN source_file_uuid;
