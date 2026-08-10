---
title: "Batches"
summary: "Message batches API for processing multiple Anthropic API requests asynchronously with local fan-out execution and results storage."
topics: [architecture]
sources:
  - id: batches-handler
    type: file
    path: internal/batches/handler.go
  - id: batches-worker
    type: file
    path: internal/batches/worker.go
  - id: batches-upstream
    type: file
    path: internal/batches/upstream.go
  - id: batches-db
    type: file
    path: internal/db/batches.go
---

Message batches provide asynchronous processing of multiple Anthropic API requests with a single API call. Rather than making sequential requests to the Anthropic Messages API, clients submit a batch containing up to hundreds of individual message requests, each identified by a custom ID. The system processes these requests in parallel using local fan-out (not forwarding to Anthropic's batch API), stores results, and provides a results endpoint for downloading completed outcomes as JSONL.

## Batch Model

A message batch consists of:

- **External ID**: Unique identifier like `msgbatch_abc123`
- **API variant**: `stable` or `beta` based on the request
- **Anthropic version**: API version header from the original request
- **Beta headers**: Optional beta feature flags passed through
- **Processing status**: Current state (`in_progress`, `ended`, `canceling`, etc.)
- **Request counts**: Tally of requests by status (processing, succeeded, errored, canceled, expired)
- **Results location**: S3 bucket and key for completed results
- **Timestamps**: Creation, expiration, completion, and archival times [@batches-db]

Each batch contains up to the configured maximum requests (default 100) with individual requests stored separately. Every request has a custom ID, request parameters, and processing status.

## Request Validation

Batch requests undergo validation before creation:

- **Custom ID pattern**: Must match `^[A-Za-z0-9_-]{1,64}$` and be unique within the batch
- **Request count**: Between 1 and configured maximum
- **Params structure**: Each request must contain valid JSON object parameters
- **Beta compatibility**: Certain beta features like `output-300k-2026-03-24` are not supported
- **Required fields**: `max_tokens` must be specified and at least 1
- **Stream rejection**: Streaming requests are not supported in batches
- **Field restrictions**: Fields like `speed`, `store`, `previous_thread_event_id`, `cache_hint`, and `context_hint` are not supported
- **Research preview**: `research_preview_2026_02` with value `active` is rejected [@batches-handler].

## Processing Lifecycle

When a batch is created, it enters the `in_progress` state with an expiration time 24 hours in the future. The system immediately enqueues a background job to process the batch [@batches-db].

The batch worker processes each request individually:

1. **Lease assignment**: Worker claims exclusive processing rights for the batch
2. **Parallel execution**: Individual requests are sent to Anthropic concurrently
3. **Result aggregation**: Responses are collected and stored as JSONL in object storage
4. **Status transition**: Batch moves to `ended` when all requests complete
5. **Result availability**: Results URL becomes accessible after successful completion [@batches-worker]

## Upstream Communication

The system uses Anthropic's official Messages API for each individual request in the batch. The upstream implementation:

- **Preserves headers**: Forwards `anthropic-version` and relevant beta headers
- **Handles streaming**: Rejects streaming requests at batch validation time
- **Processes responses**: Converts Anthropic responses into batch result format
- **Manages errors**: Captures and categorizes upstream errors appropriately [@batches-upstream]

Each request in the batch gets its own upstream API call with the parameters provided in the batch creation. Results are aggregated into a JSONL format with one line per custom ID.

## Status Tracking

Batches progress through several states:

- **in_progress**: Initial state, requests are being processed
- **canceling**: Cancellation requested, worker will stop processing new requests
- **ended**: All requests completed (succeeded, errored, or expired)
- **archived**: Batch has been archived and results are no longer available

Request-level statuses include:
- **processing**: Currently being executed
- **succeeded**: Completed successfully
- **errored**: Failed with an error response
- **canceled**: Canceled before completion
- **expired**: Batch expired before request could be processed [@batches-db]

## Cancellation

Clients can request batch cancellation via `DELETE /batches/{message_batch_id}`. Cancellation is cooperative:

1. Batch status transitions to `canceling`
2. A job is enqueued to signal the worker
3. Worker stops processing new requests when it sees the canceling state
4. Already-completed requests are preserved
5. In-progress requests may complete or be canceled depending on timing [@batches-handler]

Batches can only be canceled while in `in_progress` state. Once ended, the batch is immutable.

## Results Storage

Completed batches store results in object storage (S3-compatible) as JSONL files:

- **Format**: One JSON object per line, each with `custom_id` and `result` fields
- **Location**: `results_url` field in batch response provides download endpoint
- **Retention**: Results expire after configured retention period (default 60 days)
- **Size limits**: Results have configurable size limits
- **Content type**: Served as `application/x-jsonl` [@batches-handler]

Results remain available only while the batch is in `ended` state and not archived. Once archived or expired, attempting to download results returns a not found error.

## API Endpoints

The message batches API provides these operations:

- `POST /messages/batches`: Create a new batch (requires `beta=true` query parameter and `anthropic-beta: message-batches-2024-09-24` header)
- `GET /messages/batches`: List batches with cursor-based pagination
- `GET /messages/batches/{message_batch_id}`: Retrieve batch status and metadata
- `DELETE /messages/batches/{message_batch_id}`: Request batch cancellation
- `GET /messages/batches/{message_batch_id}/results`: Download completed results as JSONL

The API uses Anthropic-compatible batch semantics but implements processing locally rather than forwarding to Anthropic's batch service [@batches-handler].
