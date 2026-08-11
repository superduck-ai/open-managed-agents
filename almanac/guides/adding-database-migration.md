---
title: "Adding Database Migration"
summary: "Create numbered goose migrations for schema changes, avoiding foreign keys and following PostgreSQL table conventions."
topics: [database, backend, migration]
sources:
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: migration-01
    type: file
    path: internal/db/migrations/00001_init.sql
  - id: migration-02
    type: file
    path: internal/db/migrations/00002_add_mcp_oauth_flows.sql
---

All database schema changes in Open Managed Agents go through numbered goose migrations in `internal/db/migrations/` [@agents-md]. Migrations are applied automatically at server startup.

## Migration File Format

Each migration is a new numbered file like `00013_add_feature.sql` with goose Up/Down annotations [@migration-01]:

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS new_feature (
    id bigint generated always as identity,
    uuid uuid not null default gen_random_uuid(),
    external_id text not null,
    created_at timestamptz not null default now(),
    constraint new_feature_id_pk primary key (id),
    constraint new_feature_uuid_key unique (uuid),
    constraint new_feature_external_id_key unique (external_id)
);

CREATE INDEX IF NOT EXISTS new_feature_created_v1_idx
    ON new_feature (created_at desc, id desc);

-- +goose Down
DROP TABLE IF EXISTS new_feature;
```

## Table Conventions

Every core business table follows these conventions [@agents-md]:
- `id bigint generated always as identity`: Internal primary key
- `uuid uuid default gen_random_uuid()`: Stable business identifier
- `external_id text`: Anthropic API-compatible ID (e.g., `file_...`)
- `organization_id bigint`: Multi-tenant scope
- `workspace_id bigint`: Workspace-level scope
- `created_at timestamptz`: Creation timestamp
- `deleted_at timestamptz`: Soft delete support

Indexes use `IF NOT EXISTS` and are named with a `v1` suffix for version management. For example, the MCP OAuth flows migration demonstrates index creation with proper naming [@migration-02].

## Foreign Key Constraints

Do not create foreign key constraints [@agents-md]. Referential integrity is maintained at the application layer through migration code, seed code, and E2E tests. This approach avoids lock contention and allows more flexible deployment strategies.

The migration process explicitly deletes any foreign keys that appear in the schema after applying migrations, ensuring the no-FK invariant holds.

## Multi-Column Constraints

When adding multi-column unique constraints or checks, use descriptive names and idempotent `CREATE INDEX IF NOT EXISTS` syntax [@migration-02]:

```sql
CREATE UNIQUE INDEX IF NOT EXISTS table_name_column_version_key
    ON table_name (column_id, version);
```

## Adding Columns

For non-nullable column additions, use a two-step migration:
1. Add the column as nullable
2. Backfill data and alter to `NOT NULL`

This allows zero-downtime deployments when existing rows need the new value.

## Testing Migrations

After creating a migration, run `go test ./... -count=1` to ensure the schema changes work correctly with existing tests [@agents-md]. The `tests/files_api_test.go` suite includes a guard test that verifies no foreign keys exist in the schema.

For E2E verification, start a fresh server with `DB_AUTO_MIGRATE=true` and verify the migration applies cleanly.
