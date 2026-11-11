-- +goose Up
ALTER TABLE builtin_skills ADD COLUMN bundled_version text;
UPDATE builtin_skills SET bundled_version = latest_version WHERE bundled;

-- +goose Down
ALTER TABLE builtin_skills DROP COLUMN bundled_version;
