-- +goose Up
DROP INDEX IF EXISTS skills_workspace_display_title_active_key;
CREATE INDEX skills_workspace_display_title_active_key ON skills (workspace_uuid, display_title) WHERE deleted_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS skills_workspace_display_title_active_key;
CREATE UNIQUE INDEX skills_workspace_display_title_active_key ON skills (workspace_uuid, display_title) WHERE deleted_at IS NULL;
