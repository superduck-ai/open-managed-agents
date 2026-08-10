---
title: "Goose Migrations"
summary: "Database schema versioning via goose, the Go migration tool, with explicit migration files and a no-foreign-key enforcement policy."
topics: [architecture, database]
sources:
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: migrations-dir
    type: file
    path: internal/db/migrations/
  - id: init-migration
    type: file
    path: internal/db/migrations/00001_init.sql
  - id: epoch-migration
    type: file
    path: internal/db/migrations/00003_add_code_session_worker_epoch.sql
  - id: delivery-migration
    type: file
    path: internal/db/migrations/00007_add_code_session_inbound_delivery_ack.sql
  - id: builtin-skills-migration
    type: file
    path: internal/db/migrations/00010_builtin_skills.sql
---

Database schema changes in Open Managed Agents are managed through goose migrations. Each schema change adds a new numbered SQL file in `internal/db/migrations/` following the pattern `000NN_description.sql` [@migrations-dir].

## Migration Convention

All schema modifications must occur through goose migrations. The `Migrate()` function applies pending migrations in sequence and then explicitly removes all foreign key constraints from the database [@agents-md]. This two-step approach ensures schema versioning while maintaining the project's no-foreign-key policy.

Migrations are never modified after application. If a correction is needed, a new migration file is created instead.

## Foreign Key Exclusion

The project deliberately avoids PostgreSQL foreign key constraints. Core tables include reference columns like `organization_id`, `workspace_id`, and `created_by_api_key_id`, but these relationships are not enforced at the database level [@agents-md]. Instead:

* Application code writes maintain referential integrity
* Migration code and seed scripts respect relationships
* E2E test coverage validates correct behavior

The `Migrate()` function deletes any foreign keys it finds after applying migrations, serving as a guard against accidental constraint introduction [@agents-md].

## Table Structure Standard

Each core business table follows a consistent structure:

* `id bigint generated always as identity` — Internal database primary key
* `uuid uuid default gen_random_uuid()` — Stable business identifier
* `external_id text` — Anthropic API-compatible ID (e.g., `file_...`, `msg_...`)

Three identifiers serve different purposes: `id` for internal joins and performance, `uuid` for stable external references, and `external_id` for SDK compatibility [@agents-md].

## Migration Examples

Code session worker tracking for CCR v2 event delivery demonstrates the migration pattern:

* `00003_add_code_session_worker_epoch.sql` — Adds `current_worker_epoch` to track worker generations
* `00007_add_code_session_inbound_delivery_ack.sql` — Adds delivery acknowledgment fields (`delivery_worker_epoch`, `received_at`, `processing_at`, `processed_at`, `last_delivery_update_at`) to `code_session_inbound_events` [@epoch-migration] [@delivery-migration]

Built-in skills support for managed agent skills runtime shows another schema evolution:

* `00010_builtin_skills.sql` — Creates `builtin_skills` and `builtin_skill_versions` tables with archive metadata (S3 bucket/key, size, sha256) and version resolution fields [@builtin-skills-migration]

Each migration includes `+goose Up` and `+goose Down` sections for reversible application.

## Identity and Access Control Schema

Platform authentication requires multi-table entity creation within transactions:

* `organizations` — Tenant containers
* `users` — User accounts with organization membership and role
* `workspaces` — Organization-scoped workspaces
* `workspace_members` — User-to-workspace role bindings
* `api_keys` — Workspace-scoped credentials

The `00001_init.sql` migration establishes these foundation tables [@init-migration]. The `platform_auth.go` implementation provides transaction-scoped insertion primitives that the `platformauth` service orchestrates for user provisioning.

## Testing Invariants

The test suite includes guards for migration invariants. E2E tests verify that schema changes work correctly with application code, and unit tests validate that the `Migrate()` function successfully removes foreign keys after migration application.
