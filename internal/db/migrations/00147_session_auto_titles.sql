-- +goose Up
ALTER TABLE sessions ADD COLUMN title_manual boolean NOT NULL DEFAULT false;
UPDATE sessions SET title_manual = true WHERE title IS NOT NULL;

-- +goose Down
ALTER TABLE sessions DROP COLUMN title_manual;
