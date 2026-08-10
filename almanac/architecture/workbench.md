---
title: "Workbench"
summary: "Workbench HTTP routes and session streaming for the platform console, including prompt management, evaluation, and Anthropic proxy."
topics: [architecture, workbench, platform-api]
sources:
  - id: workbench-routes
    type: file
    path: internal/workbench/console_platform_workbench.go
  - id: workbench-aliases
    type: file
    path: internal/workbench/aliases.go
  - id: workbench-boundaries
    type: file
    path: docs/design/be/http-platform-workbench-boundaries.md
---

The Workbench subsystem handles HTTP routes for the platform console's prompt authoring, evaluation, and Anthropic API proxying. It operates under `/api/organizations/{orgUuid}/workbench` and provides both persistent storage through the database and in-memory fallback for local development.

## Route Organization

Workbench routes are registered through `RegisterOrgWorkbenchRoutes`, which mounts under the organization-scoped path prefix [@workbench-aliases]. The routes include prompt CRUD operations, revision management, key-value storage, evaluation tracking, and Anthropic API completion proxying.

All workbench handlers require organization visibility validation through `visibleWorkbenchOrg`, which checks that the authenticated principal can access the requested organization [@workbench-routes]. Platform Claude mirror organizations are also supported through alias handling, enabling integration with the upstream platform console.

## Prompt Management

The workbench prompt system stores prompt metadata, revisions, and key-value data. Prompts have an `id` (UUID), `name`, `workspace_id`, and `is_shared_with_workspace` visibility flag [@workbench-routes]. Revisions contain the full prompt configuration as JSONB, including system prompt, messages, tools, variables, and model settings.

Prompt listing returns both persisted prompts from the database and in-memory prompts from local state, with deleted prompts filtered out [@workbench-routes]. A default prompt with a fixed UUID is always included unless explicitly deleted. Revision listing compiles revisions from storage, evaluations, and a hardcoded default revision.

The key-value store provides draft revision storage and other transient data per prompt. Operations support optimistic concurrency through version fields and handle special keys like `draft_revision` with normalization logic.

## Evaluation System

Workbench evaluations track test case results for prompt revisions. Each evaluation has a unique ID, links to a revision, and stores variable values, golden answers, completion text, and ratings in a JSONB payload [@workbench-routes].

Evaluation creation stores results for later retrieval, and updates can modify completion text, ratings, and golden answers. Deletion returns the deleted evaluation record for confirmation. Evaluations are persisted to the database and cached in local memory for the current request context.

## Anthropic API Proxying

The `/workbench/completions` endpoint proxies requests to the Anthropic Messages API, performing several transformations [@workbench-routes]. Variable substitution replaces `{{variable}}` placeholders in system prompts and messages with values from the request payload. Tools are rewritten to rename MCP integration tools with the `mcp__{integration}__{name}` pattern.

The proxy supports streaming responses and passes through Anthropic headers including beta features. Authentication uses an upstream API token from environment variables, and the endpoint is configured through `ANTHROPIC_UPSTREAM_BASE_URL` or defaults to the Anthropic API.

## Test Case Generation

The workbench includes test case generation for prompt evaluation using the Anthropic API. The `/workbench/evaluations/generate_test_case` endpoint generates a single test case with variable values, while `/workbench/metaprompt/generate_test_cases` generates multiple cases at once [@workbench-routes].

Generation constructs an Anthropic request with a system prompt that defines the output format (XML for single cases, JSON for batch) and a user prompt that includes the variable names and prompt context. When the upstream API is unavailable, the system falls back to generating placeholder values.

## Model Metadata

The `/models` endpoint returns available Claude models with their context windows and rate limit groups [@workbench-routes]. Rate limit information is provided through `/rate_limits_v2`, which includes per-model rate limits for different operation types. Workspace-specific rate limits are available through `/workspaces/{workspaceId}/rate_limits`.

The model list includes Fable, Opus, Sonnet, and Haiku variants with their corresponding model group names for rate limiting purposes. Each model specifies maximum input and output tokens along with support flags for features like thinking and tool use.

## Platform Boundaries

The workbench subsystem maintains clear boundaries with the rest of the application. It uses the `internal/httpapi` package for JSON responses and errors but does not register its own HTTP middleware or resource routes [@workbench-boundaries]. Route mounting occurs through `RegisterOrgWorkbenchRoutes`, which is called from the main API server.

Persistence operations use a `workbenchPersistenceStore` interface that the main database implements, allowing the workbench to function with either database persistence or in-memory fallback. The in-memory state uses `sync.Map` structures for concurrent access without locks.

## Platform Console Integration

The workbench includes support for platform Claude host detection through `isPlatformClaudeHost`, which identifies requests from `platform.claude.com` or its subdomains [@workbench-routes]. This enables organization alias mapping where platform console organizations can reference local organizations through different UUID formats.

Rate limit responses use model group display names that match platform Claude expectations, and error responses maintain compatibility with the platform console's expected JSON structure. This integration allows the workbench to serve as a backend for both the standalone console and platform console interfaces.
