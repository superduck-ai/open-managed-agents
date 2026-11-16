-- +goose Up
ALTER TABLE platform_oauth_providers ADD COLUMN enabled boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE platform_oauth_providers DROP COLUMN enabled;
