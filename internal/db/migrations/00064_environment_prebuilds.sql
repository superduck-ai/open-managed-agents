-- +goose Up
ALTER TABLE environments ADD COLUMN build_job_id bigint;

-- +goose Down
ALTER TABLE environments DROP COLUMN build_job_id;
