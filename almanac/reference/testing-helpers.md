---
title: "Testing Helpers"
summary: "Testing helpers provide fixtures, test utilities, and convenience functions for writing integration and E2E tests across the managed agents codebase."
topics: [reference, testing]
sources:
  - id: sessions-fixtures
    type: file
    path: internal/sessions/fixtures.go
  - id: sessions-api-test
    type: file
    path: tests/sessions_api_test.go
  - id: test-package
    type: file
    path: tests/my_test.go
  - id: agents-handler
    type: file
    path: internal/agents/handler.go
---

# Testing Helpers

The testing helpers in this codebase provide fixtures, test utilities, and convenience functions for writing integration and E2E tests. These helpers are primarily located in `tests/` and various `fixtures.go` files throughout the codebase.

## Sessions Fixtures

The `internal/sessions/fixtures.go` file provides official SDK fixtures for session testing[@sessions-fixtures]. These fixtures ensure the official SDK has stable test data that matches real API responses.

**Fixture Detection**: The `isOfficialSDKFixturePrincipal`, `createUsesOfficialFixtures`, and related functions check whether a request uses the official SDK fixture API key and matches expected fixture IDs[@sessions-fixtures].

**Fixture Session**: The `fixtureDBSession` function returns a `db.Session` struct populated with test data including a fixture agent snapshot, metadata, vault IDs, and standard field values[@sessions-fixtures].

**Fixture Response**: The `fixtureSession` function returns a `sessionResponse` with proper external IDs, timestamps, agent snapshot, resources, and optional archived status[@sessions-fixtures].

**Fixture Resources and Threads**: Separate fixture functions generate `fixtureResource` (file mounts) and `fixtureThread` (primary thread) with consistent test data[@sessions-fixtures].

**Event Normalization**: The `normalizeFixtureEvent` function adds `id` and `processed_at` fields to fixture events, ensuring they match real event payloads[@sessions-fixtures].

## Test Application

The `tests/my_test.go` file provides a `testApp` helper that encapsulates the full server stack for integration testing[@test-package]. This helper:

- Creates a test database connection
- Initializes all HTTP handlers
- Provides cleanup via `close()` method
- Supports both real database and in-memory fakes

Test functions use `newTestAppWithStore` to create a fully functional server for making real HTTP requests during tests.

## Sessions API Tests

The `tests/sessions_api_test.go` file contains comprehensive integration tests for the sessions API[@sessions-api-test]. Key test scenarios include:

**Success paths**: Session creation, retrieval, listing, updating, archiving, and deletion with valid data

**Resources and threads**: Attaching file resources and memory stores, listing and retrieving session threads

**Event sending and listing**: Sending user messages and tool confirmations, paginating event history

**Status synchronization**: Verifying that agent status changes propagate to public session status

**Archive and delete**: Testing that archived sessions reject new events and deleted sessions return 404

**Environment key access**: Validating environment-scoped API keys work correctly

## Agent Fixtures

The agents handler includes fixture generation for SDK testing[@agents-handler]. The `fixtureAgent` function returns an `agentResponse` with:

- Fixed external ID from configuration
- Example MCP servers, tools, and skills
- Configurable version and archived status
- Standard model configuration

These fixtures enable official SDK tests to run against predictable agent data without requiring real database records.

## Memory Store Fixtures

Tests that involve memory stores use helper functions to create, retrieve, and delete memory stores[@sessions-api-test]. These helpers exercise the full memory store API surface and validate that sessions can reference memory stores correctly.

## File Resource Fixtures

File upload tests create files with specific content types and content, then attach them to sessions as resources[@sessions-api-test]. The upload helper returns the file ID for use in session creation, and cleanup functions delete the file after the test completes.

## Cursor-Based Pagination Tests

Pagination tests verify that cursor-based paging works correctly for sessions, threads, and events[@sessions-api-test]. Tests check:

- Correct ordering (ascending/descending)
- Proper next page cursor generation
- Stable results across pages
- Edge cases like empty results and single-page results

## Error Case Testing

Test helpers validate error responses for[@sessions-api-test]:

- Missing beta headers
- Invalid JSON bodies
- Unknown resource references
- Duplicate resource IDs
- Invalid status transitions
- Malformed cursors

Error assertions use the `assertError` helper to check both status code and error type match expected values.

## Cleanup Helpers

Tests register cleanup functions to delete created resources (agents, sessions, environments, files, memory stores) after the test completes[@sessions-api-test]. This ensures test isolation even when tests fail mid-execution.

Cleanup helpers typically use direct database access via `app.db` to bypass API-level soft deletes and ensure complete removal.

## API Key and Principal Helpers

Test setup creates test API keys and attaches them to request contexts for authenticated requests[@sessions-api-test]. The `defaultTestKey` constant provides a standard test key, and helpers create additional keys for testing workspace isolation and permission boundaries.

## Database Assertions

Some tests use direct database queries to assert internal state beyond what the API exposes[@sessions-api-test]. Common assertions include:

- Verifying session work state is "queued" after creation
- Checking that event timestamps were populated correctly
- Confirming that soft delete flags are set on archive/delete
- Validating that session_events table has expected records

These database assertions ensure the API responses accurately reflect persisted state.

## Workspace and Organization Tests

Multi-tenant tests validate that workspace and organization boundaries are enforced[@sessions-api-test]. Tests create resources in different workspaces and verify:

- Cross-workspace queries return 404
- Workspace-scoped API keys can't access other workspaces
- Organization-level isolation is preserved

## Environment Variable Configuration

Test helpers respect environment variables for test configuration, allowing tests to run against different database backends or with varying timeout settings without code changes.
