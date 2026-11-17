-- +goose Up
ALTER TABLE channel_personal_vaults RENAME TO channel_user_vaults;

-- +goose Down
ALTER TABLE channel_user_vaults RENAME TO channel_personal_vaults;
