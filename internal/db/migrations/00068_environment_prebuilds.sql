-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS build_job_id bigint;

-- +goose Down
ALTER TABLE environments DROP COLUMN build_job_id;
