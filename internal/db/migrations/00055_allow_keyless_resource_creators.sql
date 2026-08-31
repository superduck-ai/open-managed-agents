-- +goose Up
-- Platform sessions have no API-key actor. Existing genuine key references are retained.
ALTER TABLE files ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE skills ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE skill_versions ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE agents ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE environments ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE vaults ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE memory_stores ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE webhook_endpoints ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE message_batches ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE deployments ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE deployment_runs ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;
ALTER TABLE sessions ALTER COLUMN created_by_api_key_uuid DROP NOT NULL;

-- This namespace was previously client-writable: never trust pre-upgrade values.
UPDATE sessions SET metadata = metadata - '_oma_runtime_user_uuid' WHERE metadata ? '_oma_runtime_user_uuid';
UPDATE deployments SET metadata = metadata - '_oma_runtime_user_uuid' WHERE metadata ? '_oma_runtime_user_uuid';

-- +goose Down
-- Deliberately fail if keyless rows exist; do not fabricate keys or delete resources.
ALTER TABLE files ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE skills ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE skill_versions ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE agents ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE environments ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE vaults ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE memory_stores ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE webhook_endpoints ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE message_batches ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE deployments ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE deployment_runs ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
ALTER TABLE sessions ALTER COLUMN created_by_api_key_uuid SET NOT NULL;
