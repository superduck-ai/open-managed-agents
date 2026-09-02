-- +goose Up
-- A withdrawn draft already used version 55 for user/API-key creator columns.
-- Goose tracks versions, not file contents, so those databases need a new version.
-- Keep legacy columns and their audit data; they no longer govern resource writes.

-- +goose StatementBegin
DO $$
DECLARE
    resource_table text;
BEGIN
    FOREACH resource_table IN ARRAY ARRAY['sessions', 'deployments'] LOOP
        -- The old constraint marks metadata as not yet protected by this migration.
        -- Without it, preserve runtime identities already written by the new code,
        -- including when this migration is reapplied after its no-op Down.
        IF EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE connamespace = current_schema()::regnamespace
                AND conrelid = format('%I.%I', current_schema(), resource_table)::regclass
                AND conname = resource_table || '_creator_check'
                AND contype = 'c'
        ) AND EXISTS (
            SELECT 1 FROM information_schema.columns
            WHERE table_schema = current_schema()
                AND table_name = resource_table
                AND column_name = 'created_by_user_uuid'
        ) THEN
            EXECUTE format($sql$
                UPDATE %I
                SET metadata = (metadata - '_oma_runtime_user_uuid')
                    || jsonb_strip_nulls(jsonb_build_object('_oma_runtime_user_uuid', created_by_user_uuid))
                WHERE created_by_user_uuid IS NOT NULL OR metadata ? '_oma_runtime_user_uuid'
            $sql$, resource_table);
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_creator_check;
ALTER TABLE deployment_runs DROP CONSTRAINT IF EXISTS deployment_runs_creator_check;
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_creator_check;
ALTER TABLE environments DROP CONSTRAINT IF EXISTS environments_creator_check;
ALTER TABLE files DROP CONSTRAINT IF EXISTS files_creator_check;
ALTER TABLE filestore_filesystems DROP CONSTRAINT IF EXISTS filestore_filesystems_creator_check;
ALTER TABLE memory_stores DROP CONSTRAINT IF EXISTS memory_stores_creator_check;
ALTER TABLE message_batches DROP CONSTRAINT IF EXISTS message_batches_creator_check;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_creator_check;
ALTER TABLE skill_versions DROP CONSTRAINT IF EXISTS skill_versions_creator_check;
ALTER TABLE skills DROP CONSTRAINT IF EXISTS skills_creator_check;
ALTER TABLE vault_credentials DROP CONSTRAINT IF EXISTS vault_credentials_creator_check;
ALTER TABLE vaults DROP CONSTRAINT IF EXISTS vaults_creator_check;
ALTER TABLE webhook_endpoints DROP CONSTRAINT IF EXISTS webhook_endpoints_creator_check;

-- +goose Down
-- Forward-only normalization of a withdrawn draft. The current version 55 also
-- permits keyless resources; restoring draft constraints would break that contract.
SELECT 1;
