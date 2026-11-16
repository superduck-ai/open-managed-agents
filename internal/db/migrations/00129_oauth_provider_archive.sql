-- +goose Up
ALTER TABLE platform_oauth_providers ADD COLUMN archived boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE platform_oauth_providers DROP COLUMN archived;
