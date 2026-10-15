-- +goose Up
ALTER TABLE session_events ALTER COLUMN processed_at DROP NOT NULL;

-- +goose Down
UPDATE session_events SET processed_at = created_at WHERE processed_at IS NULL;
ALTER TABLE session_events ALTER COLUMN processed_at SET NOT NULL;
