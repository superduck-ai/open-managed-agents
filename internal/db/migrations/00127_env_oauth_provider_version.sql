-- +goose Up
ALTER TABLE env_oauth_flows ADD COLUMN provider_version bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE env_oauth_flows DROP COLUMN provider_version;
