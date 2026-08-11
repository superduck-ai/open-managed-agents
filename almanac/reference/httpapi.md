---
title: "HTTP API"
summary: "The httpapi package provides shared utilities for HTTP request handling, response formatting, and request context management."
topics: [api]
sources:
  - id: httpapi-helpers
    type: file
    path: internal/httpapi/handler_helpers.go
  - id: httpapi-errors
    type: file
    path: internal/httpapi/errors.go
  - id: httpapi-request
    type: file
    path: internal/httpapi/request.go
  - id: httpapi-tests
    type: file
    path: internal/httpapi/handler_helpers_test.go
---

The httpapi package provides utility functions shared across all HTTP handlers. It handles JSON decoding, metadata normalization, pagination parameter parsing, time formatting, and request context propagation. These utilities ensure consistent API behavior and reduce code duplication across resource handlers.

## JSON Body Processing

The `DecodeObjectBody()` function reads and parses JSON request bodies with a size limit enforced via `http.MaxBytesReader`. It returns a `map[string]json.RawMessage` representing the top-level object, allowing handlers to selectively decode individual fields. The function validates that the body is a JSON object (not an array or primitive) and returns descriptive errors for invalid JSON or non-object bodies [@httpapi-helpers].

## Metadata Handling

Metadata fields use a specialized `map[string]string` representation with helper functions for normalization and patching. `NormalizeMetadata()` converts raw JSON to a validated string map, converting `null` to an empty object and optionally calling a validator function. `PatchMetadata()` applies partial updates using JSON merge semantics, where `null` values delete keys and string values add or update keys. `ValidateMetadataEntryLimit()` checks metadata entry count against a maximum. These functions ensure metadata fields remain consistent across the API [@httpapi-helpers][@httpapi-tests].

## Time Formatting

All timestamps use RFC3339 format in UTC. `FormatTime()` converts a `time.Time` to a formatted string, and `OptionalTime()` handles pointer timestamps, returning `nil` for nil pointers. The formatting explicitly converts to UTC before formatting, ensuring consistent timezone handling regardless of server location [@httpapi-helpers].

## Pagination and Query Parameters

`ParseLimit()` extracts and validates the `limit` query parameter, defaulting to 20 and enforcing a maximum value. `ParseOptionalTime()` parses RFC3339 timestamp parameters for filtering, returning nil when the parameter is absent. These functions centralize query parameter validation logic and provide consistent error messages [@httpapi-helpers][@httpapi-tests].

## Request Context

The request context package manages request ID propagation through the middleware chain. `WithRequestID()` stores a request ID in the context, and `RequestID()` retrieves it. This request ID appears in error responses and logs, enabling distributed tracing and debugging. The context key is a private struct type to prevent collisions [@httpapi-request].

## Response Writing

The `WriteJSON()` helper sets the `Content-Type` header to `application/json`, writes the status code, and encodes the response body. This is used by `WriteError()` for error responses and can be used directly for successful responses. The unified helper ensures consistent JSON encoding behavior across all handlers [@httpapi-errors].
