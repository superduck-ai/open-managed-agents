---
title: "Memory Stores"
summary: "Persistent key-value storage for agent sessions with versioning, path-based organization, and content integrity verification."
topics: [architecture]
sources:
  - id: memory-handler
    type: file
    path: internal/memory/handler.go
  - id: memory-db
    type: file
    path: internal/db/memory.go
---

Memory stores provide persistent key-value storage that agent sessions can read from and write to during execution. Unlike ephemeral conversation context, memory stores persist across sessions and support versioning for tracking changes over time. Memory is organized in a file-system-like structure with paths, and supports content integrity verification via SHA-256 hashes.

## Memory Store Model

A memory store is a workspace-scoped container for memories:

- **External ID**: Unique identifier like `memstore_abc123`
- **Name**: Human-readable identifier (1-255 characters, no control characters)
- **Description**: Optional detailed description (up to 1024 characters)
- **Metadata**: Key-value pairs for organization (up to 16 entries, keys up to 64 bytes, values up to 512 bytes)
- **Archived at**: Optional timestamp when the store was archived [@memory-handler]

Memory stores are soft-deleted and support archiving for retention while preventing new writes.

## Memory Paths

Memories are organized by filesystem-like paths:

- **Format**: Must start with `/`, no empty segments (no `//`), no trailing slash
- **Characters**: Valid UTF-8, NFC-normalized, no control or format characters
- **Segments**: Cannot contain `.` or `..` as path components
- **Length**: Up to 1024 bytes total
- **Uniqueness**: Paths must be unique within a memory store (case-sensitive) [@memory-handler]

Path conflicts are detected at write time. If a write attempts to use a path already occupied by different content (based on SHA-256), the operation fails with a conflict error containing the conflicting memory ID and path.

## Memory Content

Memory content has several size and format constraints:

- **Content size**: Up to 100KB (102,400 bytes) per memory
- **Storage**: Stored as text in object storage (S3-compatible)
- **Integrity**: SHA-256 hash stored for content verification
- **Versioning**: Each write creates a new version with actor tracking [@memory-handler]

Content is stored in object storage keyed by workspace, store, memory, and version UUIDs. The database retains a reference to the object location for retrieval.

## Memory Versions

Every memory write creates a new version record:

- **Operation type**: `created`, `modified`, or `deleted`
- **Path**: The memory path at time of operation
- **Content size and hash**: Size and SHA-256 of content (null for deleted)
- **Actor**: Who made the change (API key, session, or user)
- **Timestamp**: When the version was created
- **Redaction**: Optional redaction timestamp and actor for sensitive data removal [@memory-db]

Versions provide an audit trail of all changes to memory within a store, supporting replay and debugging of agent behavior.

## Memory Operations

The memory API supports several operations:

**Create:**
- `POST /memory_stores/{memory_store_id}/memories`
- Requires path and content
- Returns the created memory with content

**Read:**
- `GET /memory_stores/{memory_store_id}/memories/{memory_id}`
- Supports `view=basic` (metadata only) or `view=full` (includes content)
- Returns current memory state

**Update:**
- `POST /memory_stores/{memory_store_id}/memories/{memory_id}`
- Can update content, path, or both
- Supports optional precondition via SHA-256 hash for optimistic locking
- Uses retry loop for handling concurrent updates
- Returns updated memory state

**Delete:**
- `DELETE /memory_stores/{memory_store_id}/memories/{memory_id}`
- Supports optional `expected_content_sha256` query parameter for safety
- Creates a deletion version record [@memory-handler]

## Precondition Checking

Memory updates support optimistic concurrency control via preconditions:

- **Format**: `{"type": "content_sha256", "content_sha256": "<hash>"}`
- **Behavior**: Update only proceeds if current content matches expected hash
- **Use case**: Prevents overwriting concurrent changes
- **Response**: Returns current state without modification if precondition fails [@memory-handler]

This allows multiple agents or sessions to work with the same memory store safely, with failed updates returning the current state for client-side conflict resolution.

## Listing and Filtering

Memory listing supports several filtering options:

- **Path prefix**: Filter memories under a specific path
- **Order**: `asc` or `desc` by path, created_at, or updated_at
- **Limit**: Page size up to 100
- **Depth**: Optional integer for prefix rollup (returns both prefixes and memories)
- **View**: `basic` (metadata only) or `full` (includes content) [@memory-handler]

The depth parameter enables filesystem-style directory listings where a depth of 1 returns immediate children (files and directories) under a path prefix.

## Version Queries

Memory versions can be queried with several filters:

- **Memory ID**: Filter versions for a specific memory
- **Operation**: Filter by operation type (created, modified, deleted)
- **Actor**: Filter by API key or session that made changes
- **Time range**: Filter by creation time bounds
- **View**: `basic` (metadata only) or `full` (includes content if not redacted) [@memory-handler]

This enables comprehensive audit trails and change history analysis for memory stores.

## Content Redaction

Memory versions can be redacted for sensitive data:

- **Endpoint**: `POST /memory_stores/{memory_store_id}/memory_versions/{memory_version_id}/redact`
- **Constraint**: Cannot redact the current (active) version of a memory
- **Effect**: Sets `redacted_at` timestamp and `redacted_by` actor
- **Storage**: Object storage content is deleted asynchronously
- **Visibility**: Redacted versions return null content in API responses [@memory-handler]

Redaction provides a way to handle sensitive data in memory while preserving the audit trail of what changed.

## API Endpoints

The memory stores API requires `beta=true` query parameter and provides:

**Memory store operations:**
- `POST /memory_stores`: Create a new memory store
- `GET /memory_stores`: List memory stores with pagination
- `GET /memory_stores/{memory_store_id}`: Retrieve a specific store
- `POST /memory_stores/{memory_store_id}`: Update store metadata
- `POST /memory_stores/{memory_store_id}/archive`: Archive a store
- `DELETE /memory_stores/{memory_store_id}`: Delete a store

**Memory operations:**
- `POST /memory_stores/{memory_store_id}/memories`: Create a memory
- `GET /memory_stores/{memory_store_id}/memories`: List memories with filtering
- `GET /memory_stores/{memory_store_id}/memories/{memory_id}`: Retrieve a memory
- `POST /memory_stores/{memory_store_id}/memories/{memory_id}`: Update a memory
- `DELETE /memory_stores/{memory_store_id}/memories/{memory_id}`: Delete a memory

**Version operations:**
- `GET /memory_stores/{memory_store_id}/memory_versions`: List versions with filtering
- `GET /memory_stores/{memory_store_id}/memory_versions/{memory_version_id}`: Retrieve a specific version
- `POST /memory_stores/{memory_store_id}/memory_versions/{memory_version_id}/redact`: Redact a version [@memory-handler]
