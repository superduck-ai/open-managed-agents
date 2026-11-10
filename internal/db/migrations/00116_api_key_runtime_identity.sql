-- +goose Up
ALTER TABLE api_keys ADD COLUMN runtime_user_uuid uuid;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN runtime_user_uuid;
