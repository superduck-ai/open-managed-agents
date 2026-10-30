-- +goose Up
ALTER TABLE usage_records ADD COLUMN provider_cost jsonb;

-- +goose Down
ALTER TABLE usage_records DROP COLUMN provider_cost;
