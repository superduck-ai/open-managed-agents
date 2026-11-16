-- +goose Up
ALTER TABLE channel_reply_deliveries ADD COLUMN artifact_delivery jsonb NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE channel_reply_deliveries DROP COLUMN artifact_delivery;
