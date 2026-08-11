---
title: "Monolithic Architecture"
summary: "Open Managed Agents uses a monolithic architecture with clear layer boundaries and vertical resource slicing."
topics: [architecture]
sources:
  - id: agents-conventions
    type: file
    path: AGENTS.md
  - id: web-conventions
    type: file
    path: web/AGENTS.md
---

Open Managed Agents is deployed as a single Go binary that combines API serving, business logic, and data persistence in one process. The architecture maintains clarity through explicit layer boundaries and vertical feature slicing rather than distributed service boundaries.

## Layer Structure

The monolith is organized into three primary layers with strict dependency rules. The API layer (`internal/api`) handles service assembly, global middleware, authentication entry points, and resource route mounting. It orchestrates requests but does not contain business rules, SQL queries, or resource-level request handling details [@agents-conventions].

Resource packages (`internal/{agents,sessions,files,memory,...}`) own the API handlers, request validation, business orchestration, and response mapping for their respective domains. Each resource corresponds to an API surface and operates independently within the monolith [@agents-conventions].

The database layer (`internal/db`) serves as the persistence boundary. It cannot import API packages, HTTP handlers, or construct HTTP responses. Database functions return ordinary Go errors or identifiable result statuses rather than HTTP-specific types [@agents-conventions].

## Dependency Direction

Dependencies flow unidirectionally from API to database. The API layer may depend on the database layer, but the database layer cannot depend on the API layer. Shared foundation packages similarly cannot depend on specific feature handler or resource packages [@agents-conventions].

This direction is enforced through architectural conventions: API request/response DTOs are not shadows of the database schema. Database row structures, API response structures, and internal business structures can map to each other, but HTTP fields must not leak into the database layer [@agents-conventions].

## Vertical Resource Slicing

Features are organized as vertical slices aligned with API resources rather than horizontal technical layers. Each resource package contains its own handlers, domain logic, and data access patterns. This approach keeps related code together and reduces cross-cutting coupling [@agents-conventions].

The frontend follows similar vertical slicing principles with `quickstart/`, `agents/`, `sessions/`, and `resources/` feature modules. Route files stay lean, delegating to focused feature modules rather than containing business logic [@agents-conventions].

## Frontend Integration

The frontend is a static Vite application served from the `web/` directory. It remains a client-side console application backed by the Go service, not a separate backend-for-frontend (BFF) layer. The Go service proxies `/api` and `/v1` requests while serving static frontend assets [@web-conventions].

API boundaries are explicitly separated: `/api/*` provides the Console API for platform operations, while `/v1/*` maintains Anthropic API compatibility. The frontend calls both APIs but must distinguish between them at the client level [@web-conventions].
