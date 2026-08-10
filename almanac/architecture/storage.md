---
title: "Storage"
summary: "Object storage implementation using MinIO for file storage, cleanup workers, and workspace-scoped access."
topics: [architecture, storage, minio]
sources:
  - id: storage-package
    type: file
    path: internal/storage/storage.go
  - id: db-package
    type: file
    path: internal/db/
  - id: files-handler
    type: file
    path: internal/files/handler.go
---

The Open Managed Agents system uses MinIO as an S3-compatible object store for file storage. The storage implementation provides a simple interface for storing, retrieving, and deleting objects while handling bucket management and endpoint configuration.

## Object Store Implementation

The `MinIOStore` struct wraps the MinIO client library and implements an `ObjectStore` interface with four methods: `EnsureBucket`, `Put`, `Get`, and `Delete` [@storage-package]. The store is configured with a bucket name, region, and connection credentials from the application configuration.

Endpoint normalization handles both plain hostnames and URLs with `http://` or `https://` schemes [@storage-package]. The `secure` boolean is inferred from the URL scheme, allowing the same configuration format to work for both local development with HTTP and production deployments with HTTPS.

Bucket creation occurs on-demand through `EnsureBucket`, which checks for bucket existence before creating [@storage-package]. This lazy creation approach supports local development where the bucket may not exist at startup.

## Storage Paths

Objects are stored with hierarchical keys that include workspace UUID and file UUID for scoping and uniqueness. The pattern `workspaces/{workspace_uuid}/files/{file_uuid}/{filename}` ensures each workspace has a distinct storage namespace while keeping files grouped within their workspace [@files-handler].

Platform-specific file variants like thumbnails use a `variants/` subdirectory within the same parent key, allowing related files to be grouped together while maintaining clean separation from the original object.

## Storage and Database Coordination

Files are uploaded to object storage before database records are created, ensuring the database never references non-existent storage [@files-handler]. When a file upload fails after the object has been stored but before the database record is written, the system attempts immediate cleanup and falls back to enqueuing a cleanup job if cleanup fails.

This ordering is critical for data consistency. The database is the source of truth for which files exist, and object storage holds the content. By writing to storage first and then creating the database record, the system guarantees that any file in the database has corresponding storage.

## Cleanup Operations

When a file is deleted, the system first performs a soft delete in the database and then attempts to delete the object from storage [@files-handler]. If storage deletion fails due to transient issues, a cleanup job is enqueued to retry later. The jobs table stores cleanup operations with the bucket, key, and resource identifiers needed for eventual cleanup.

This approach handles partial failures gracefully. A file may be marked deleted in the database immediately, making it unavailable to API clients, while storage cleanup occurs asynchronously. The jobs table tracks pending cleanup operations, and background workers process these jobs to free storage space.

## Storage Limits

Workspace storage quotas are enforced during file creation. The `CreateFileIfWithinLimit` database method checks the current storage usage against the workspace limit before allowing new files [@db-package]. This enforcement happens after the object has been uploaded to storage but before the database record is created, allowing the system to clean up the uploaded object if the quota would be exceeded.

The storage limit check sums the `size_bytes` of all non-deleted files in the workspace, providing an accurate accounting of storage usage. When the limit is exceeded, the upload fails with a `permission_error` response, and the uploaded object is cleaned up.
