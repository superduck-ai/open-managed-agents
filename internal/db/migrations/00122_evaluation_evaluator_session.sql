-- +goose Up

ALTER TABLE evaluation_evaluator_versions
    ADD COLUMN environment_external_id text NOT NULL DEFAULT '',
    ADD COLUMN resources jsonb;
ALTER TABLE evaluation_evaluator_versions ADD CONSTRAINT evaluation_evaluator_versions_environment_len
    CHECK (char_length(environment_external_id) <= 255);
ALTER TABLE evaluation_evaluator_versions ADD CONSTRAINT evaluation_evaluator_versions_resources_array
    CHECK (resources IS NULL OR jsonb_typeof(resources) = 'array');

-- +goose Down

ALTER TABLE evaluation_evaluator_versions DROP CONSTRAINT IF EXISTS evaluation_evaluator_versions_resources_array;
ALTER TABLE evaluation_evaluator_versions DROP CONSTRAINT IF EXISTS evaluation_evaluator_versions_environment_len;
ALTER TABLE evaluation_evaluator_versions DROP COLUMN IF EXISTS resources;
ALTER TABLE evaluation_evaluator_versions DROP COLUMN IF EXISTS environment_external_id;
