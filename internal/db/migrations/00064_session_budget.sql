-- +goose Up
ALTER TABLE sessions ADD COLUMN budget jsonb,
    ADD COLUMN budget_reached_at timestamptz,
    ADD COLUMN budget_removed_at timestamptz;
ALTER TABLE deployments ADD COLUMN budget jsonb;

-- +goose Down
ALTER TABLE deployments DROP COLUMN budget;
ALTER TABLE sessions DROP COLUMN budget_removed_at,
    DROP COLUMN budget_reached_at,
    DROP COLUMN budget;
