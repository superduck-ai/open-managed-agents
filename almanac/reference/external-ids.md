---
title: "External IDs"
summary: "The ids package generates prefixed, base62-encoded identifiers for API resources."
topics: [architecture]
sources:
  - id: ids-package
    type: file
    path: internal/ids/ids.go
  - id: files-ids
    type: file
    path: internal/files/handler.go
  - id: deployments-ids
    type: file
    path: internal/deployments/handler.go
  - id: code-sessions-ids
    type: file
    path: internal/codesessions/service.go
---

External IDs are human-readable, URL-safe identifiers used throughout the API to reference resources. The `ids.New()` function generates these IDs with a resource-specific prefix and a random suffix, providing stable identifiers that work across database migrations and are compatible with Anthropic API conventions.

## ID Format

Each external ID consists of a type prefix followed by 24 random characters using a base62-like alphabet (`0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz`). For example, a file ID might be `file_abc123XYZ...` where `file_` is the prefix and the remaining characters are randomly generated. The randomness comes from `crypto/rand.Read()`, ensuring uniqueness and unpredictability [@ids-package].

## Generation Process

The `ids.New(prefix)` function allocates a 24-byte buffer, fills it with cryptographically secure random bytes, then maps each byte to a character from the 62-character alphabet using modulo arithmetic. The prefix is concatenated with the encoded bytes to form the complete ID. If random byte generation fails, the function returns an error wrapped with context [@ids-package].

## Common Prefixes

Different resource types use distinct prefixes to make IDs self-describing. Files use `file_`, deployments use `dep_`, deployment runs use `drun_`, sessions use `sesn_`, threads use `sthr_`, work items use `work_`, session events use `sevt_`, outcomes use `outc_`, session resources use `sesrsc_`, and code sessions use `cse_`. These prefixes match Anthropic API conventions for resource identification [@files-ids][@deployments-ids][@code-sessions-ids].

## Usage in Handlers

Handlers generate external IDs when creating new resources. For example, when uploading a file, the handler calls `ids.New("file_")` to generate a unique file ID. If generation fails, the handler returns an HTTP 500 error. Generated IDs are stored in the database `external_id` column and returned to clients in API responses, allowing clients to reference the resource in subsequent requests [@files-ids][@deployments-ids].

## Database Storage

The database schema stores external IDs in a text `external_id` column on each core table. While the primary key is an auto-incrementing bigint `id` column, the `external_id` serves as the stable business identifier exposed via the API. This separation allows internal IDs to change (such as during data migrations) without affecting API clients. External IDs are unique within their resource type and are used in API URLs and request bodies [@deployments-ids].
