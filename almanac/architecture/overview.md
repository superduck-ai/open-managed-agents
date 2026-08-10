---
title: "Architecture Overview"
summary: "High-level system architecture, monolith structure, and major boundaries."
topics: [architecture, system-design, boundaries]
sources:
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: main-go
    type: file
    path: main.go
---

Open Managed Agents is a monolithic Go backend with a React/Vite frontend that implements the Anthropic Managed Agents API with Claude Code runtime integration. The system uses a pragmatic domain-driven design approach with clear vertical boundaries and dependency directions.

## Monolith Structure

The monolith is organized into vertical resource slices rather than horizontal architectural layers:

- **`internal/api/`**: Service assembly, global middleware, credential routing, and resource mounting. No business rules or SQL lives here.
- **`internal/{agents,sessions,files,memory,...}/`**: Resource packages containing handlers, request validation, business orchestration, and response mapping.
- **`internal/db/`**: Persistence boundary that cannot import API or HTTP layers. Returns plain Go errors, not HTTP responses.
- **`internal/platformapi/`**: Platform console API routes for organization/workspace management, billing, and admin functions.
- **`internal/httpapi/`**: Shared HTTP utilities including error formatting compatible with Anthropic's API. [@agents-md]

The dependency rule is clear: API layers may depend on DB layers, but DB layers cannot depend on API layers or resource handlers.

## Request Flow

A request flows through the system in stages:

1. **Entry Point**: `main.go` creates the HTTP server with `go-chi/chi/v5` routing and 10-minute timeouts [@main-go]
2. **Global Middleware**: Request ID injection, panic recovery, and logging
3. **Credential Routing**: `/v1/*` requests are routed to service or platform APIs based on API key vs session cookie presence
4. **Authentication**: Service or platform auth middleware validates credentials and injects principal context
5. **Resource Handler**: Business logic executes with database access through the persistence boundary
6. **Response**: HTTP responses are formatted using `httpapi` utilities for Anthropic compatibility

## Background Workers

Several background workers run concurrently:

- **Batch Worker**: Processes message batches with expiry sweeps
- **Environment Runner**: Manages E2B environment lifecycle
- **Skill Prewarm Worker**: Fans out skill volume prewarming on version changes
- **Webhook Worker**: Delivers webhook events with retry logic
- **Object Cleanup Worker**: Periodically cleans up unused objects from MinIO [@main-go]

## Database Schema

PostgreSQL stores all persistent data with these conventions:

- No foreign key constraints (integrity enforced at application layer)
- Every table uses `id bigint generated always as identity` as internal primary key
- Every table has `uuid uuid default gen_random_uuid()` for stable business identifiers
- External IDs (`external_id text`) provide Anthropic API compatibility (e.g., `file_...`) [@agents-md]

Migrations are managed through goose and numbered sequentially (`00001_init.sql`, `00002_...`).

## External Services

The system integrates with several external services:

- **PostgreSQL**: Primary database for all persistent data
- **Redis**: Platform session storage with configurable TTL
- **MinIO**: Object storage for skill archives, files, and memory stores
- **E2B**: Sandbox environment runtime for Claude Code sessions

## Frontend Architecture

The React frontend (in `web/`) uses:

- **Vite**: Build tool and dev server
- **shadcn/ui**: Component library with `new-york` style
- **TanStack Router**: Client-side routing
- **TanStack Query**: Server state management
- **Feature-sliced design**: Vertical feature organization with `app/`, `pages/`, `widgets/`, `features/`, `entities/`, and `shared/` layers [@agents-md]

The frontend is statically built and served, with all API calls going through the `/v1/*` or `/api/*` backends.
