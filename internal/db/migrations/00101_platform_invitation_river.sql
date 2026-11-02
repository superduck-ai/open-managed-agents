-- +goose Up
ALTER TABLE platform_admin_invite_deliveries DROP COLUMN attempts;

-- +goose Down
ALTER TABLE platform_admin_invite_deliveries ADD COLUMN attempts integer NOT NULL DEFAULT 0;
