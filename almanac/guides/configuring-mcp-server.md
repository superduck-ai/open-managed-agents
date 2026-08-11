---
title: "Configuring MCP Server"
summary: "Configure Model Context Protocol servers for integration with managed agents, including authentication and best practices."
topics: [mcp, integration, configuration]
sources:
  - id: mcp-best-practices
    type: file
    path: assets/skills/examples/mcp-builder/reference/mcp_best_practices.md
  - id: mcp-oauth-migration
    type: file
    path: internal/db/migrations/00002_add_mcp_oauth_flows.sql
---

Model Context Protocol (MCP) servers extend agent capabilities by providing tools and resources. Configuration involves server implementation, authentication setup, and registration with managed agents through vault credentials or OAuth flows.

## Server Implementation

MCP servers can be implemented in Python or Node/TypeScript. Python servers use naming convention `{service}_mcp` (e.g., `slack_mcp`), while Node/TypeScript servers use `{service}-mcp-server` (e.g., `slack-mcp-server`)[@mcp-best-practices]. Names should be descriptive, avoid version numbers, and be inferable from task descriptions.

Tool names follow `{service}_{action}_{resource}` format with snake_case. Examples include `slack_send_message` and `github_create_issue`[@mcp-best-practices]. Tool descriptions must precisely match functionality and include annotations like `readOnlyHint`, `destructiveHint`, `idempotentHint`, and `openWorldHint`.

## Transport Options

Servers support two transport modes:

- **Streamable HTTP** — Best for remote servers, web services, and multi-client scenarios with bidirectional communication[@mcp-best-practices]
- **stdio** — Best for local integrations and command-line tools using standard input/output streams[@mcp-best-practices]

Transport selection affects deployment model (remote vs local), client capacity (multiple vs single), complexity, and real-time capabilities[@mcp-best-practices].

## Response Formats

Tools that return data should support both JSON and Markdown formats. JSON provides machine-readable structured data for programmatic processing, while Markdown offers human-readable formatted text[@mcp-best-practices]. The `response_format` parameter controls output type.

## Pagination

Listing tools must implement pagination with `limit` parameter respect, `offset` or cursor-based progression, and metadata including `has_more`, `next_offset`/`next_cursor`, and `total_count`[@mcp-best-practices]. Never load entire datasets into memory—default to 20-50 items per page.

## Security Practices

Authentication uses OAuth 2.1 with certificates from recognized authorities or API keys stored in environment variables[@mcp-best-practices]. Validate access tokens before processing requests and only accept tokens intended for your server.

Input validation must sanitize file paths to prevent directory traversal, validate URLs and external identifiers, check parameter sizes and ranges, and prevent command injection[@mcp-best-practices]. Use schema validation with Pydantic or Zod for all inputs.

For streamable HTTP servers running locally, enable DNS rebinding protection, validate the `Origin` header, and bind to `127.0.0.1` rather than `0.0.0.0`[@mcp-best-practices].

## OAuth Flow Configuration

MCP OAuth flows use the `mcp_oauth_flows` table for authorization state tracking[@mcp-oauth-migration]. The flow captures:

- Authorization and token endpoints
- Client credentials and auth method
- PKCE code verifier and challenge
- Redirect URL and display name
- Status tracking (`pending`, `completed`, `failed`)

Completed flows create vault credentials for ongoing MCP server access. The OAuth flow expires after a configurable timeout, with status tracking and error recording for debugging[@mcp-oauth-migration].

## Error Handling

Use standard JSON-RPC error codes and report tool errors within result objects rather than protocol-level errors[@mcp-best-practices]. Provide helpful, specific error messages with suggested next steps without exposing internal implementation details. Clean up resources properly after errors.

## Testing Requirements

Comprehensive testing covers functional verification with valid and invalid inputs, integration testing with external systems, security testing for auth, input sanitization, and rate limiting, performance testing under load with timeouts, and error handling validation[@mcp-best-practices].
