-- +goose Up
CREATE INDEX brain_image_attachments_session_cleanup ON brain_image_attachments (session_uuid, workspace_uuid, created_at) WHERE state IN ('pending', 'attached');
CREATE INDEX sessions_brain_image_cleanup ON sessions (uuid, workspace_uuid) WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX sessions_brain_image_cleanup;
DROP INDEX brain_image_attachments_session_cleanup;
