---
title: "Errors"
summary: "The httpapi package defines a standard error structure compatible with Anthropic API conventions."
topics: [api, errors]
sources:
  - id: httpapi-errors
    type: file
    path: internal/httpapi/errors.go
  - id: db-errors
    type: file
    path: internal/db/db.go
  - id: server-errors
    type: file
    path: internal/api/server.go
  - id: files-errors
    type: file
    path: internal/files/handler.go
---

The application uses a structured error model that maintains compatibility with Anthropic API error responses. The `httpapi.Error` type encapsulates HTTP status codes, error types, and human-readable messages, while database operations return domain-specific errors that handlers map to appropriate HTTP responses.

## HTTP Error Structure

The `httpapi.Error` type contains three fields: `Status` (HTTP status code), `Type` (string error type identifier), and `Message` (human-readable description). The `NewError()` function constructs errors with these fields, and `WriteError()` serializes them to JSON responses with the standard Anthropic-compatible format including a `request_id` field [@httpapi-errors].

## Error Response Format

All error responses follow a consistent JSON structure with top-level `type` set to `"error"`, a `request_id` field for tracing, and a nested `error` object containing `type` and `message` fields. This format matches Anthropic API conventions, allowing SDKs to parse errors consistently regardless of which service returns them [@httpapi-errors].

## Common Error Types

Handlers use specific error types to indicate different failure categories. `invalid_request_error` (HTTP 400) signals malformed requests, missing required fields, or invalid parameter values. `authentication_error` (HTTP 401) indicates missing or invalid API keys. `permission_error` (HTTP 403) represents authorization failures such as accessing archived workspaces or exceeding storage limits. `not_found_error` (HTTP 404) indicates requested resources do not exist. `api_error` (HTTP 500) represents internal server errors [@server-errors][@files-errors].

## Database Errors

Database operations return typed errors from `internal/db`. `ErrNotFound` indicates a query returned no rows and maps to HTTP 404. `ErrStorageLimitExceeded` maps to HTTP 403 with a permission error type. `ErrInvalidState`, `ErrPreconditionFailed`, `ErrDuplicate`, and `ErrVersionConflict` represent various constraint violations. Worker-related errors include `ErrWorkerEpochMismatch`, `ErrWorkerNotRegistered`, `ErrWorkerLeaseExpired`, and `ErrLimitExceeded` [@db-errors][@files-errors].

## Error Mapping in Handlers

Handlers check for specific database errors using `errors.Is()` and map them to appropriate HTTP responses. For example, when file metadata creation fails with `ErrStorageLimitExceeded`, the handler returns HTTP 403 with `permission_error` type. When database lookups fail with `ErrNotFound`, handlers return HTTP 404 with `not_found_error` type. All other unexpected errors typically map to HTTP 500 with `api_error` type while logging the underlying issue [@files-errors].

## Panic Recovery

The API server includes panic recovery middleware that converts panics to HTTP 500 responses. When a panic occurs, the middleware logs the panic with the request ID and returns a standard `api_error` response. This prevents stack traces from leaking to clients while preserving error information for server-side debugging [@server-errors].
