-- +goose Up
-- Kept as a no-op for prerelease branches where this version previously
-- contained worker-state convergence. 00004 is now the canonical state migration.
select 1;

-- +goose Down
select 1;
