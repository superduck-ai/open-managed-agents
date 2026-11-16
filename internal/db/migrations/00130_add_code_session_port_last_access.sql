-- +goose Up
ALTER TABLE code_sessions ADD COLUMN IF NOT EXISTS port_last_access_at timestamptz;

-- +goose Down
ALTER TABLE code_sessions DROP COLUMN IF EXISTS port_last_access_at;
