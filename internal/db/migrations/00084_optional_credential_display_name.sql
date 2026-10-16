-- +goose Up
ALTER TABLE vault_credentials DROP CONSTRAINT vault_credentials_display_name_length;
ALTER TABLE vault_credentials ADD CONSTRAINT vault_credentials_display_name_length CHECK (char_length(display_name) <= 255);

-- +goose Down
ALTER TABLE vault_credentials DROP CONSTRAINT vault_credentials_display_name_length;
ALTER TABLE vault_credentials ADD CONSTRAINT vault_credentials_display_name_length CHECK (char_length(display_name) BETWEEN 1 AND 255);
