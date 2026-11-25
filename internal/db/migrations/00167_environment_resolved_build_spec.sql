-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS resolved_build_spec jsonb;

-- +goose Down
ALTER TABLE environments DROP COLUMN IF EXISTS resolved_build_spec;
