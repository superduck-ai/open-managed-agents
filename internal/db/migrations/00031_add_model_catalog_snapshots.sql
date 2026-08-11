-- +goose Up

-- A catalog snapshot is global because the configured BYOK provider credentials
-- are global. It deliberately has no foreign keys and retains the last complete
-- provider result when a later refresh fails.
create table if not exists model_catalog_snapshots (
    id bigint generated always as identity primary key,
    uuid uuid not null default gen_random_uuid() unique,
    catalog_key text not null unique,
    models jsonb not null default '[]'::jsonb,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    last_error text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

-- +goose Down

drop table if exists model_catalog_snapshots;
