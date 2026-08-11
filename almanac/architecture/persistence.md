---
title: "Persistence"
summary: "Persistence layer patterns, transaction handling, and data access abstraction in the Open Managed Agents monolith."
topics: [architecture, persistence, database]
sources:
  - id: db-package
    type: file
    path: internal/db/
  - id: storage-package
    type: file
    path: internal/storage/storage.go
  - id: files-handler
    type: file
    path: internal/files/handler.go
  - id: agents-convention
    type: file
    path: AGENTS.md
---

The persistence layer in Open Managed Agents follows a disciplined abstraction where database operations never construct HTTP responses and HTTP handlers never contain SQL. This boundary keeps the persistence layer testable and reusable while maintaining clear separation of concerns.

## Database Package Boundary

The `internal/db/` package is the persistence boundary [@agents-convention]. It cannot import `internal/api`, `internal/httpapi`, or any handler packages. It must not construct HTTP status codes, HTTP responses, or Anthropic-compatible error JSON. These responsibilities belong strictly to the API layer.

Database queries return ordinary Go errors or recognizable domain errors like `ErrNotFound`, `ErrDuplicate`, `ErrVersionConflict`, and `ErrStorageLimitExceeded` [@db-package]. API handlers map these errors to appropriate HTTP responses through `internal/httpapi.WriteError`. This keeps the database layer unaware of HTTP semantics while enabling rich error handling at the edges.

The database connection is managed through a `*DB` struct that wraps a `pgxpool.Pool` [@db-package]. Connection pooling is configured with a maximum of 10 connections per pool, balancing concurrency with database resource limits. The pool is created during application startup and closed on shutdown.

## Object Store Interface

Object storage uses a `MinIOStore` that implements an `ObjectStore` interface with methods for `Put`, `Get`, `Delete`, and `EnsureBucket` [@storage-package]. The store wraps the MinIO client library and handles both MinIO and S3-compatible endpoints through endpoint normalization.

The storage layer is similarly isolated from HTTP concerns. Object operations return Go errors, and handlers map these to HTTP responses. Files are uploaded to object storage before database metadata records are created, ensuring the database never references non-existent storage [@files-handler].

## Transaction Patterns

Multi-step operations that involve multiple tables or both database and storage changes use explicit transactions. The `tx.Begin()` pattern establishes a transaction boundary, and operations commit only if all steps succeed [@db-package]. This pattern is critical for operations like creating files, where the object must be stored successfully before the database record is written.

When a file upload fails after the object has been stored but before the database record is created, the system enqueues a cleanup job rather than leaving orphaned objects [@files-handler]. This ensures eventual consistency without requiring distributed transactions across storage and database systems.

The code session event delivery system uses transactions to ensure exactly-once processing. Inbound events are inserted with unique constraints on `sequence_num` and `idempotency_key`, and delivery status updates occur within the same transaction context to prevent lost events.

## Error Mapping

Persistence errors map to domain concepts before reaching the API layer. The `ErrNotFound` error indicates a missing resource, `ErrDuplicate` indicates a constraint violation, and `ErrVersionConflict` indicates concurrent modification attempts [@db-package]. The `ErrStorageLimitExceeded` error specifically maps to workspace quota enforcement.

For workbench operations, the persistence layer provides a `workbenchPersistenceStore` interface with methods for upserting prompts, revisions, key-value records, and evaluations. Store methods return typed records or `ErrNotFound` when records don't exist. The workbench HTTP handlers map these to appropriate responses.

## Workspace Scoping

Every persistence query that accesses tenant-scoped data must include `organization_id` and `workspace_id` filters. This requirement is enforced through consistent query patterns rather than database-level row-level security. The application layer constructs queries with these filters based on the authenticated principal's context.

The persistence layer never performs authorization decisions. It returns data based on the filters provided by the caller. Authorization checks occur in the API layer before persistence methods are invoked, ensuring the persistence layer remains focused on data access rather than security policy.
