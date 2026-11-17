-- +goose Up
ALTER TABLE evaluation_cases
    ADD COLUMN origin text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE evaluation_cases
    DROP COLUMN origin;
