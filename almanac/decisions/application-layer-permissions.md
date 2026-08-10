---
title: "Application-Layer Permissions"
summary: "Authorization and access control decisions enforced at the API and service layers rather than within database queries."
topics: [architecture, security]
sources:
  - id: auth-boundaries
    type: file
    path: docs/design/be/db-platform-auth-boundaries.md
  - id: platform-auth
    type: file
    path: internal/platformauth/service.go
  - id: db-platform-auth
    type: file
    path: internal/db/platform_auth.go
  - id: api-server
    type: file
    path: internal/api/server.go
---

Permission checks in Open Managed Agents reside in the application layer (API handlers and services), not in database queries. The database package provides data access primitives and transaction boundaries, while authorization logic remains a responsibility of calling code.

## Layer Responsibilities

The `internal/db` package handles database connections, migrations, seeds, transaction management, and SQL query execution primitives [@auth-boundaries]. It exposes focused transaction-scoped access functions such as:

* Querying users by email for platform authentication
* Inserting organizations, users, workspaces, workspace members, and API keys
* Session identity resolution

Authorization decisions—whether a user may perform a specific action—live outside the database layer [@auth-boundaries].

## Platform Authentication Flow

Magic-link login demonstrates the application-layer boundary. The `internal/platformauth` package orchestrates the domain flow while delegating data access to `db.WithPlatformAuthTx`:

1. Email normalization (empty email defaults to `test@qq.com`)
2. User context lookup within a transaction
3. Default organization, admin user, default workspace, workspace_admin membership, and active API key creation if no user exists
4. Transaction commit only after all operations succeed

All these entities insert within a single database transaction via `PlatformAuthTxStore`, but the decision to create them and the default value assignment occur in `platformauth.Service` [@platform-auth] [@db-platform-auth].

## API Layer Enforcement

HTTP request authentication occurs in the API layer's middleware before handler invocation. The `serviceAuthMiddleware` and `platformAuthMiddleware` functions:

* Extract credentials (`Authorization` header for API keys, `sessionKey` cookie for platform sessions)
* Validate credentials against the database
* Construct an `auth.Principal` containing organization, workspace, and user identifiers
* Attach the principal to the request context for downstream handlers

The database lookup here returns raw row data; the middleware converts that into the application's principal representation and makes the authentication decision [@api-server].

## Multi-Tenant Isolation

Multi-tenant boundaries require explicit scoping in API handler queries. All workspace and organization-scoped resource queries must include `organization_id` and `workspace_id` predicates. The database does not enforce these constraints automatically—handlers must supply them to prevent cross-tenant access via external identifier lookups [@auth-boundaries].

The database layer provides column definitions (`organization_id`, `workspace_id`, `created_by_api_key_id`) but does not add foreign key constraints. Referential integrity relies on application-layer writes and test coverage rather than database-enforced constraints.

## Cross-Layer Dependencies

The architecture enforces unidirectional dependencies:

* `internal/platformauth` may depend on `internal/db` data access interfaces
* `internal/db` must not depend on `internal/platformauth`, `internal/platformapi`, or HTTP handler packages
* `internal/api` assembles `platformauth.Service` and passes it to `platformapi.RegisterPlatformEmailLoginRoutes`

This prevents database concerns from leaking authorization logic, and prevents authorization details from contaminating the data access layer [@auth-boundaries].

## Workspace Role Assignment

Default workspace roles are assigned by `platformauth.Service` during auto-provisioning. The `createDefaultUserOrganization` function specifies `workspace_role: "workspace_admin"` when inserting the initial workspace member. This default value originates in the domain logic, not the database schema [@platform-auth].

User organization-level roles (`admin`, `billing`, etc.) also reside in application layer logic. The database stores the role string, but deciding which role to assign during user creation is an application concern.
