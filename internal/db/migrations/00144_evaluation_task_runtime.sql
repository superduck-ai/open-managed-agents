-- +goose Up
UPDATE evaluation_tasks SET status = 'running', updated_at = now() WHERE status = 'pending';

ALTER TABLE evaluation_tasks
    DROP COLUMN IF EXISTS strategy,
    DROP COLUMN IF EXISTS low_score_threshold,
    DROP COLUMN IF EXISTS optimize_system,
    DROP COLUMN IF EXISTS baseline_system,
    DROP COLUMN IF EXISTS candidate_system,
    DROP COLUMN IF EXISTS candidate_summary,
    DROP COLUMN IF EXISTS applied_at;

ALTER TABLE evaluation_task_scores
    ADD COLUMN IF NOT EXISTS judge_input_tokens bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS judge_output_tokens bigint NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS evaluation_task_items_unstarted_idx
    ON evaluation_task_items (updated_at, uuid)
    WHERE status = 'pending' OR (status = 'running' AND session_external_id = '');

-- +goose Down
DROP INDEX IF EXISTS evaluation_task_items_unstarted_idx;

ALTER TABLE evaluation_task_scores
    DROP COLUMN IF EXISTS judge_output_tokens,
    DROP COLUMN IF EXISTS judge_input_tokens;

ALTER TABLE evaluation_tasks
    ADD COLUMN IF NOT EXISTS strategy text NOT NULL DEFAULT '' CHECK (strategy IN ('', 'safe', 'targeted', 'deep')),
    ADD COLUMN IF NOT EXISTS low_score_threshold integer NOT NULL DEFAULT 60 CHECK (low_score_threshold >= 0 AND low_score_threshold <= 100),
    ADD COLUMN IF NOT EXISTS optimize_system boolean NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS baseline_system text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS candidate_system text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS candidate_summary text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS applied_at timestamptz;
