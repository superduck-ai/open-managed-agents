-- +goose Up
ALTER TABLE session_events ALTER COLUMN processed_at DROP NOT NULL;

-- +goose Down
ALTER TABLE session_events ALTER COLUMN processed_at SET NOT NULL;
