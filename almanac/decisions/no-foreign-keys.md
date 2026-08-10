---
title: "No Foreign Keys"
summary: "The database schema avoids foreign key constraints, maintaining referential integrity at the application layer."
topics: [architecture, database]
sources:
  - id: agents-conventions
    type: file
    path: AGENTS.md
  - id: db-schema
    type: file
    path: internal/db/migrations/00001_init.sql
---

Open Managed Agents intentionally avoids PostgreSQL foreign key constraints despite maintaining referential relationships between entities. This design shifts integrity enforcement to the application layer, providing flexibility for migration, testing, and deployment scenarios.

## Schema Convention

The database migration system explicitly removes all foreign key constraints after applying schema changes. The `Migrate()` function runs goose migrations and then deletes any foreign key constraints discovered in the current schema [@agents-conventions]. A guard test in `tests/files_api_test.go` ensures no foreign keys are accidentally introduced.

Core tables retain reference columns such as `organization_id`, `workspace_id`, and `created_by_api_key_id` as `bigint` fields. These columns store the same values that foreign keys would reference, but without the database-level constraint enforcement [@agents-conventions].

## Application-Layer Integrity

Referential integrity is maintained through application code rather than database constraints. The migration code, seed scripts, and end-to-end tests collectively ensure that references remain valid [@agents-conventions]. This approach allows for controlled data operations without database enforcement overhead.

The convention applies uniformly across the schema. For example, the `workspaces` table contains an `organization_id` column but no foreign key constraint to the `organizations` table. Similarly, `files` references `workspace_id` and `created_by_api_key_id` without database-enforced relationships [@db-schema].

## Table Pattern

Every core business table follows a consistent primary key pattern. The `id` column uses `bigint generated always as identity` as the internal database primary key. A `uuid` column defaults to `gen_random_uuid()` for stable business identifiers. An `external_id` column stores Anthropic API-compatible identifiers such as `file_...` prefixes [@agents-conventions].

This three-identifier pattern separates internal database mechanics from stable business identifiers and public API contracts. Internal queries can use efficient integer primary keys, while external operations work with stable UUIDs or API-format identifiers [@db-schema].

## Migration Workflow

All schema changes must flow through `internal/db/migrations` as numbered goose migration files. Each change gets a new migration file with a name like `00002_add_xxx.sql`. Previously applied migrations are never modified, and schema changes are not appended to `internal/db/schema.go` [@agents-conventions].

This clean migration approach, combined with the absence of foreign keys, supports safer rollbacks and easier schema evolution across deployment environments.
