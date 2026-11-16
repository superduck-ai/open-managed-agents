-- +goose Up
ALTER TABLE platform_oauth_providers ADD COLUMN workspace_uuid uuid;
ALTER TABLE platform_oauth_providers ADD CONSTRAINT platform_oauth_providers_workspace_check CHECK (workspace_uuid IS NULL OR builtin_key IS NULL);
DROP INDEX platform_oauth_providers_custom_name_key;
CREATE UNIQUE INDEX platform_oauth_providers_custom_name_key ON platform_oauth_providers(name) WHERE builtin_key IS NULL AND workspace_uuid IS NULL;
CREATE UNIQUE INDEX workspace_oauth_providers_name_key ON platform_oauth_providers(workspace_uuid,name) WHERE workspace_uuid IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Workspace OAuth clients cannot be downgraded; restore the database and key backup'; END $$;
-- +goose StatementEnd
