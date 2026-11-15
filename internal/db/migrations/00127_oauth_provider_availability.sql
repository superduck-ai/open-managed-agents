-- +goose Up
ALTER TABLE platform_oauth_providers ADD COLUMN enabled boolean NOT NULL DEFAULT true;

UPDATE platform_oauth_providers
SET default_secret_name = 'FRIDAY_4A_TOKEN', version = version + 1, updated_at = NOW()
WHERE builtin_key = 'friday' AND default_secret_name = '';

-- +goose Down
ALTER TABLE platform_oauth_providers DROP COLUMN enabled;
