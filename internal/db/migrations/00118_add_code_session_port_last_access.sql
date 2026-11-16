-- +goose Up
ALTER TABLE code_sessions ADD COLUMN port_last_access_at timestamptz;

-- +goose Down
ALTER TABLE code_sessions DROP COLUMN port_last_access_at;
