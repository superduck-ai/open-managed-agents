-- +goose NO TRANSACTION

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS users_login_identity_idx;
CREATE INDEX CONCURRENTLY users_login_identity_idx ON users (LOWER(email), registration_identity DESC, added_at, uuid);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS users_login_identity_idx;
