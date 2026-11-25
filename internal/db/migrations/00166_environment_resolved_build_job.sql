-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS resolved_build_job_id bigint;

UPDATE environments
SET resolved_build_job_id = build_job_id
WHERE build_job_id IS NOT NULL
  AND resolved_template <> ''
  AND resolved_build_job_id IS NULL;

-- +goose Down
UPDATE environments
SET resolved_template = ''
WHERE build_job_id IS NOT NULL
  AND resolved_build_job_id IS DISTINCT FROM build_job_id;
ALTER TABLE environments DROP COLUMN IF EXISTS resolved_build_job_id;
