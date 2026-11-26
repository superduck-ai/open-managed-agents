-- +goose Up
ALTER TABLE environments ADD COLUMN IF NOT EXISTS resolved_build_job_id bigint;
ALTER TABLE environments ADD COLUMN IF NOT EXISTS resolved_packages jsonb;

UPDATE environments
SET resolved_build_job_id = build_job_id,
    resolved_packages = jsonb_build_object(
        'type', 'packages',
        'apt', CASE WHEN jsonb_typeof(config #> '{packages,apt}') = 'array' THEN config #> '{packages,apt}' ELSE '[]'::jsonb END,
        'cargo', CASE WHEN jsonb_typeof(config #> '{packages,cargo}') = 'array' THEN config #> '{packages,cargo}' ELSE '[]'::jsonb END,
        'gem', CASE WHEN jsonb_typeof(config #> '{packages,gem}') = 'array' THEN config #> '{packages,gem}' ELSE '[]'::jsonb END,
        'go', CASE WHEN jsonb_typeof(config #> '{packages,go}') = 'array' THEN config #> '{packages,go}' ELSE '[]'::jsonb END,
        'npm', CASE WHEN jsonb_typeof(config #> '{packages,npm}') = 'array' THEN config #> '{packages,npm}' ELSE '[]'::jsonb END,
        'pip', CASE WHEN jsonb_typeof(config #> '{packages,pip}') = 'array' THEN config #> '{packages,pip}' ELSE '[]'::jsonb END
    )
WHERE build_job_id IS NOT NULL
  AND resolved_template <> ''
  AND resolved_build_job_id IS NULL;

-- +goose Down
UPDATE environments
SET resolved_template = ''
WHERE build_job_id IS NOT NULL
  AND resolved_build_job_id IS DISTINCT FROM build_job_id;
ALTER TABLE environments DROP COLUMN IF EXISTS resolved_packages;
ALTER TABLE environments DROP COLUMN IF EXISTS resolved_build_job_id;
