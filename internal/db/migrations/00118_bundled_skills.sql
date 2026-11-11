-- +goose Up
ALTER TABLE builtin_skills ADD COLUMN bundled boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE builtin_skills DROP COLUMN bundled;
