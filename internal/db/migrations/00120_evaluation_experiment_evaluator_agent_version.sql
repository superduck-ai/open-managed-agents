-- +goose Up

ALTER TABLE evaluation_experiment_evaluators ADD COLUMN agent_version integer;
ALTER TABLE evaluation_experiment_evaluators ADD CONSTRAINT evaluation_experiment_evaluators_agent_version_positive
    CHECK (agent_version IS NULL OR agent_version > 0);

-- +goose Down

ALTER TABLE evaluation_experiment_evaluators DROP CONSTRAINT IF EXISTS evaluation_experiment_evaluators_agent_version_positive;
ALTER TABLE evaluation_experiment_evaluators DROP COLUMN IF EXISTS agent_version;
