---
title: "Files API"
summary: "Anthropic-compatible Files API implementation for upload, download, listing, and deletion of workspace-scoped files."
topics: [architecture, api, files]
sources:
  - id: files-handler
    type: file
    path: internal/files/handler.go
  - id: platform-files
    type: file
    path: internal/files/platform.go
---

The Files API provides Anthropic-compatible file upload, download, listing, and deletion operations. Files are stored in workspace-scoped storage with metadata tracked in PostgreSQL and content stored in MinIO. The API requires the `anthropic-beta: files-api-2025-04-14` header for all operations.

## Upload Handling

File uploads accept multipart form data with a `file` field [@files-handler]. The handler validates the filename length and UTF-8 encoding, detects MIME type from the `Content-Type` header or file extension, and enforces size limits through `http.MaxBytesReader`.

During upload, the file content is streamed to MinIO storage while computing a SHA-256 hash [@files-handler]. The storage key uses the pattern `workspaces/{workspace_uuid}/files/{file_uuid}/{sanitized_filename}`, ensuring workspace isolation and filename sanitization replaces unsafe characters with underscores.

After successful storage, a database record is created with `CreateFileIfWithinLimit`, which enforces workspace storage quotas [@files-handler]. If the quota is exceeded, the uploaded object is cleaned up before returning an error. If metadata creation fails for any other reason, the object is also cleaned up, with a fallback to enqueuing a cleanup job if immediate cleanup fails.

## Platform Upload Endpoint

The platform console provides a separate `/upload_b64` endpoint that accepts base64-encoded file uploads [@platform-files]. This endpoint handles JSON payloads with `file_name`, `file_b64`, and optional `file_kind` fields. Base64 decoding supports multiple variants including standard, URL-safe, and data URL formats.

For image uploads, the platform endpoint generates thumbnails at a maximum edge of 400 pixels, storing them as variants alongside the original file [@platform-files]. The thumbnail is stored at `variants/thumbnail.{ext}` within the same directory as the original file. Image metadata including dimensions, primary color, and asset URLs is returned in the upload response.

## Download and Content Serving

File downloads are available through the `/files/{file_id}/content` endpoint, but only for files where `downloadable` is true [@files-handler]. Most files in the system are not directly downloadable through the API and are instead accessed indirectly through tool use.

The platform provides separate `/files/{file_uuid}/thumbnail` and `/files/{file_uuid}/preview` endpoints that serve file variants with cache-control headers set to one week [@platform-files]. These endpoints check organization-scoped access and stream content directly from storage, with thumbnail variants falling back to the original image when a separate thumbnail is not available.

## Listing and Pagination

The `/files` endpoint lists files in the authenticated workspace with cursor-based pagination using `after_id` and `before_id` parameters [@files-handler]. A default limit of 20 files applies, with a maximum of 1000. The response includes `first_id` and `last_id` for cursor navigation and `has_more` for detecting additional pages.

Files can be filtered by `scope_id` to retrieve only files associated with a specific session or other scoped context. The workspace ID is always enforced through the authenticated principal, preventing cross-workspace access.

## Deletion and Cleanup

File deletion uses soft deletes in the database, setting `deleted_at` rather than removing records [@files-handler]. After the database soft delete succeeds, the system attempts to delete the object from MinIO storage. If storage deletion fails, a cleanup job is enqueued for eventual retry.

For platform-uploaded files with thumbnails, deletion also attempts to remove the thumbnail variant. If thumbnail deletion fails, a separate cleanup job is enqueued for the variant resource. This ensures that failed cleanup doesn't prevent the logical deletion from succeeding while still allowing eventual storage reclamation.

## Workspace Scoping

All file operations are scoped to the authenticated workspace through the principal's `WorkspaceID` and `WorkspaceUUID` [@files-handler]. File uploads record the creating API key in `created_by_api_key_id` for audit trail. Queries always filter by workspace ID and deleted status to enforce both multi-tenant isolation and soft deletion semantics.

The platform endpoints include additional organization-level validation, ensuring the requested organization matches the principal's organization before processing requests. This extra check prevents access through incorrect organization identifiers in the URL path.
