-- +goose Up
ALTER TABLE skills ADD COLUMN icon_key text NOT NULL DEFAULT 'blocks';
ALTER TABLE builtin_skills
    ADD COLUMN icon_key text NOT NULL DEFAULT 'blocks',
    ADD COLUMN bundled boolean NOT NULL DEFAULT false,
    ADD COLUMN bundled_version text;

-- +goose Down
ALTER TABLE builtin_skills
    DROP COLUMN bundled_version,
    DROP COLUMN bundled,
    DROP COLUMN icon_key;
ALTER TABLE skills DROP COLUMN icon_key;
