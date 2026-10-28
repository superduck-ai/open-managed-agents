-- +goose Up
ALTER TABLE usage_records ADD COLUMN model_cost_usd text;

-- +goose Down
ALTER TABLE usage_records DROP COLUMN model_cost_usd;
