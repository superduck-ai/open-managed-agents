---
title: "Auth Boundaries"
summary: "Authentication boundaries, credential routing, and platform vs service auth separation."
topics: [architecture, auth, security]
sources:
  - id: server-go
    type: file
    path: internal/api/server.go
  - id: auth-go
    type: file
    path: internal/auth/auth.go
  - id: db-platform-auth
    type: file
    path: docs/design/be/db-platform-auth-boundaries.md
  - id: agents-md
    type: file
    path: AGENTS.md
---

# Auth Boundaries

The Open Managed Agents system maintains clear authentication boundaries between platform console operations and service API operations. These boundaries are enforced through credential-based routing, separation of concerns between packages, and multi-tenant scoping at the data access layer.

## Credential Types

The [`internal/auth`][@auth-go] package defines three credential types:

| Credential Type | Source | Purpose |
|-----------------|--------|---------|
| `api_key` | `X-Api-Key` header or `Authorization: Bearer` | Service API authentication |
| `environment_key` | Same as api_key, checked on specific paths | Environment-scoped operations |
| `platform_session` | `sessionKey` cookie | Platform console authentication |

The [`auth.Principal`][@auth-go] structure carries resolved authentication context including organization, workspace, and user identifiers. Principals are attached to request contexts via [`auth.WithPrincipal()`][@auth-go] and retrieved via [`auth.PrincipalFromContext()`][@auth-go].

## Platform Auth Package Boundaries

The [`docs/design/be/db-platform-auth-boundaries.md`][@db-platform-auth] document defines clear responsibility boundaries:

**`internal/db`** retains database connection, migrations, seed, transaction, and SQL query primitives. It exposes only data access functions needed by platform auth, such as user lookup by email and organization/user/workspace/API key creation within transactions.

**`internal/platformauth`** carries magic-link login domain orchestration including email normalization, default naming, external ID generation, and default creation flows. It maintains transaction consistency across organization, user, workspace, member, and API key creation via [`db.WithPlatformAuthTx`][@db-platform-auth].

**`internal/platformapi`** handles only HTTP request parsing, cookie/session response writing, and route registration. Magic-link verify operations retrieve user context and session identity from [`platformauth.Service`][@db-platform-auth] and account data from bootstrap stores.

## Service Authentication Flow

Service API authentication in [`internal/api/server.go`][@server-go] follows [`authenticateService()`][@server-go]:

1. Extract API key via [`auth.ExtractAPIKey()`][@auth-go]
2. Hash the key using [`auth.HashAPIKey()`][@auth-go] (SHA-256)
3. Perform database lookup for hashed key
4. If not found and path is environment credential path, lookup environment key
5. Return principal with organization, workspace, and API key details

Environment keys extend authentication to specific environment scopes for operations like sessions and skills.

## Platform Authentication Flow

Platform authentication via [`authenticatePlatformSession()`][@server-go] includes session recovery logic:

1. Extract `sessionKey` cookie via [`auth.ExtractPlatformSessionKey()`][@auth-go]
2. Retrieve session from platform store
3. Resolve organization UUID if missing
4. Apply organization override from header or query parameter
5. Apply workspace override if provided
6. Archive workspace check for overridden workspace

The [`recoverPlatformMirrorSession()`][@server-go] function handles expired sessions by attempting to recover session context from bootstrap data, creating new sessions when valid user/org context exists.

## Multi-Tenant Boundaries

Authentication boundaries extend to multi-tenant data access. All database queries include organization and workspace scope identifiers from authenticated principals. The [`AGENTS.md`][@agents-md] convention explicitly states:

> "Multi-tenant boundary must be explicit: all workspace/org level resource queries and writes must carry `organization_id`, `workspace_id` or corresponding external scope, avoiding queries solely by external_id that would cause privilege escalation."

This prevents cross-tenant data access through API requests. The database layer does not enforce these boundaries—responsibility lies with API/resource handlers to include scope identifiers in all queries.

## Platform Mirror Organization Alias

Platform sessions support organization aliasing via [`auth.WithPlatformMirrorOrganizationAlias()`][@auth-go]. This allows platform UI operations to reference organizations by alias while maintaining authenticated context. The [`platformMirrorOrganizationAlias()`][@server-go] function validates alias membership before allowing override.

## Dependency Direction

Auth boundaries maintain clear dependency directions per the platform auth design:

- [`internal/platformauth`][@db-platform-auth] depends on [`internal/db`][@db-platform-auth] data access interfaces
- [`internal/db`][@db-platform-auth] does not depend on platform auth or HTTP handler packages
- [`internal/api`][@server-go] assembles [`platformauth.Service`][@db-platform-auth] and passes it to platform route registration
- [`internal/api`][@server-go] does not contain business rules, SQL, or resource-level handling details

This separation allows independent evolution of authentication logic, data access, and HTTP handling.
