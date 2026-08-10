---
title: "Database"
summary: "PostgreSQL schema, migration patterns, and query organization for the Open Managed Agents monolith."
topics: [architecture, database, postgresql]
sources:
  - id: init-migration
    type: file
    path: internal/db/migrations/00001_init.sql
  - id: db-package
    type: file
    path: internal/db/
  - id: goose-migrations
    type: file
    path: internal/db/migrations.go
  - id: agents-convention
    type: file
    path: AGENTS.md
---

The Open Managed Agents monolith uses PostgreSQL as its primary data store. The database schema is managed through goose migrations and uses a distinctive no-foreign-key architecture with application-layer referential integrity.

## Schema Organization

Every table follows a three-identifier pattern: an internal `bigint` primary key, a stable `uuid`, and an `external_id` text field for API compatibility [@init-migration]. The `external_id` values match Anthropic API formats like `file_`, `session_`, and `agent_` [@agents-convention]. This separation allows internal database operations to use efficient integer joins while exposing stable, API-compatible identifiers to clients.

Core multi-tenant resources all include `organization_id` and `workspace_id` columns, establishing the scoping boundary for access control. Every query that returns user-visible data must filter by these columns to prevent cross-tenant data leakage [@agents-convention].

Tables use `deleted_at` NULL/NOT NULL for soft deletion rather than hard deletes. This provides audit trails and enables recovery, but queries must always include `WHERE deleted_at IS NULL` filters to exclude deleted records unless explicitly accessing deleted state.

## Migration Management

Database migrations are managed through the goose migration tool with versioned SQL files in `internal/db/migrations/` [@goose-migrations]. The `Migrate()` function runs goose migrations and then explicitly removes any foreign key constraints that may have been added [@db-package].

The no-foreign-key decision is deliberate: foreign keys are dropped after every migration run [@db-package]. Referential integrity is maintained at the application layer through careful transaction ordering and validation. This avoids lock contention during cascading deletes and allows the system to handle orphaned records gracefully during distributed operation.

## Key Resource Tables

The `organizations` and `workspaces` tables form the multi-tenant foundation. Organizations contain `settings` and `profile` JSONB columns for flexible configuration [@init-migration]. Workpaces include `compartment_id` for isolation, `display_color` for UI presentation, and `data_residency` for geo-policy configuration.

The `api_keys` table stores hashed API keys with `key_hash` as a unique constraint. The `status` column supports `active` and `revoked` states, while `created_by_user_id` traces key provenance. Console-specific API keys use the separate `console_api_keys` table with additional fields like `key_prefix`, `key_suffix`, and `partial_key_hint` for secure display.

The `files` table stores file metadata with the actual content in object storage. Columns include `filename`, `mime_type`, `size_bytes`, `sha256` for integrity verification, and `s3_bucket`/`s3_key` for object storage location [@init-migration]. The `scope_type` and `scope_id` columns enable session-scoped or workspace-scoped files.

The `sessions` table represents managed agent execution sessions with `status` values of `rescheduling`, `running`, `idle`, or `terminated` [@init-migration]. Each session captures an `agent_snapshot` as JSONB, preserving the exact agent configuration at creation time. Sessions link to environments through `environment_id` and to agents through `agent_id` and `agent_version`.

## Code Session Tables

The `code_sessions` table tracks Claude Code runtime sessions with worker state management. The `connection_status` field tracks whether a worker is connected, while `last_inbound_sequence_num` and `last_outbound_sequence_num` enable reliable event delivery ordering [@init-migration].

Event delivery uses three tables: `code_session_inbound_events` for events from workers to the platform, and `code_session_outbound_events` for events from the platform to workers. Both use `sequence_num` for ordering and `idempotency_key` for deduplication. The `delivery_status` field on inbound events tracks whether each event has been successfully delivered to its destination.

## Workbench Tables

The Workbench prompt management system uses four tables. `workbench_prompts` stores prompt metadata with `is_shared_with_workspace` for visibility control and `latest_revision_uuid` pointing to the current revision [@init-migration]. The `workbench_prompt_revisions` table stores the full prompt configuration as JSONB in the `payload` column, enabling complete revision history.

The `workbench_prompt_kv` table provides a key-value store per prompt for draft revisions and other transient data. The `workbench_evaluations` table stores evaluation results linked to revisions, with the full evaluation state in the `payload` JSONB column.

## Query Patterns

Database queries in this codebase avoid ORM-style abstractions. The `internal/db/` package contains focused query functions that return domain-specific structs and well-known errors like `ErrNotFound` and `ErrDuplicate` [@db-package]. Queries use parameterized SQL throughout to prevent injection and enable prepared statement caching.

Multi-table writes always use explicit transactions with `tx.Begin()` and `tx.Commit()` patterns. The transaction boundary ensures that related state changes succeed or fail together, particularly important for operations like creating sessions with associated resources or updating agent versions with corresponding snapshot records.
