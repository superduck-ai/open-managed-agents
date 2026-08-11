---
title: "API Architecture"
summary: "HTTP routing, middleware, and resource mounting for the Open Managed Agents service."
topics: [architecture, api, routing]
sources:
  - id: server-go
    type: file
    path: internal/api/server.go
  - id: httpapi-errors
    type: file
    path: internal/httpapi/errors.go
  - id: httpapi-helpers
    type: file
    path: internal/httpapi/handler_helpers.go
  - id: auth-go
    type: file
    path: internal/auth/auth.go
  - id: agents-md
    type: file
    path: AGENTS.md
---

# API Architecture

The Open Managed Agents API is built on [Go-Chi/Chi v5][@server-go] for HTTP routing and middleware. The system implements two distinct API surfaces: a platform console API for web UI operations and an Anthropic-compatible service API for managed agents. Both surfaces share a common [`/v1/*` resource layer][@server-go] but are separated by authentication credentials and routing logic.

## Entrypoint Routing

The HTTP server exposes three primary route groups at [`internal/api/server.go`][@server-go]:

- `/healthz` - Simple health check endpoint
- `/v1` and `/v1/*` - Anthropic-compatible service API with dual authentication
- `/api`, `/auth`, `/oauth`, `/web-api` - Platform console API

The [`v1EntrypointRouter()`][@server-go] implements credential-based routing by examining incoming requests:

```go
func (r apiEntrypointRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
    // Route by authentication credential.
    if auth.ExtractAPIKey(req) != "" {
        r.service.ServeHTTP(w, req)
        return
    }
    if auth.ExtractPlatformSessionKey(req) != "" {
        r.platform.ServeHTTP(w, req)
        return
    }
    r.platform.ServeHTTP(w, req)
}
```

Service API requests use [`X-Api-Key`][@auth-go] or `Authorization: Bearer` headers, while platform requests use the `sessionKey` cookie. Missing credentials route to the platform path for unauthenticated operations like magic-link login.

## Service API Router

The [`serviceAPIRouter()`][@server-go] mounts shared v1 resources under [`serviceAuthMiddleware`][@server-go]:

```go
func (s *Server) serviceAPIRouter() chi.Router {
    router := chi.NewRouter()
    router.Route("/v1", func(r chi.Router) {
        r.Use(s.serviceAuthMiddleware)
        r.NotFound(notFound)
        r.MethodNotAllowed(notFound)
        s.mountSharedV1Resources(r)
    })
    return router
}
```

Service authentication validates API keys via [`authenticateService()`][@server-go], which hashes the provided key and performs database lookup. Environment keys are checked when API key lookup fails, matching against credential-specific paths like [`/v1/environments/*/work`][@server-go].

## Platform API Router

The [`platformAPIRouter()`][@server-go] handles platform console operations:

```go
func (s *Server) platformAPIRouter() chi.Router {
    router := chi.NewRouter()
    router.Route("/v1", func(r chi.Router) {
        r.NotFound(notFound)
        r.MethodNotAllowed(notFound)
        platformapi.RegisterPlatformPrivacyConsentRoutes(r)
        r.Group(func(r chi.Router) {
            r.Use(s.platformAuthMiddleware)
            s.mountPlatformV1Resources(r)
        })
    })
    return router
}
```

Platform authentication uses [`authenticatePlatformSession()`][@server-go] with session recovery logic and organization/workspace override capabilities.

## Resource Mounting

Both service and platform APIs mount shared resources via [`mountSharedV1Resources()`][@server-go]:

```go
func (s *Server) mountSharedV1Resources(r chi.Router) {
    r.Post("/agents:search", s.agents.Search)
    r.Mount("/agents", s.agents)
    r.Mount("/deployment_runs", s.deploymentRuns)
    r.Mount("/deployments", s.deployments)
    r.Mount("/environments", s.envs)
    r.Mount("/files", s.files)
    r.Mount("/memory_stores", s.memory)
    r.Mount("/messages/batches", s.batch)
    r.Mount("/models", s.models)
    r.Mount("/organizations", s.admin)
    r.Mount("/sessions", s.sessions)
    r.Mount("/skills", s.skills)
    r.Mount("/vaults", s.vaults)
    r.Mount("/webhooks", s.webhooks)
}
```

Each mounted handler is a `chi.Router` that implements its own resource-level sub-routes using Go-Chi's routing groups and sub-router capabilities.

## Middleware Stack

Global middleware applied in [`NewServerWithPlatformSessions()`][@server-go]:

1. **Request ID middleware** - Injects `request-id` header or generates `req_*` IDs
2. **Request logging middleware** - Logs HTTP requests when logger provided
3. **Panic recovery middleware** - Catches panics and returns `api_error` responses
4. **Not found handler** - Returns `not_found_error` for unmatched routes

Error responses use [`internal/httpapi.WriteError()`][@httpapi-errors] to maintain Anthropic-compatible format:

```go
func WriteError(w http.ResponseWriter, r *http.Request, err *Error) {
    WriteJSON(w, err.Status, map[string]any{
        "type":       "error",
        "request_id": RequestID(r.Context()),
        "error": map[string]string{
            "type":    err.Type,
            "message": err.Message,
        },
    })
}
```

## Handler Conventions

Handlers follow standard [`net/http` boundaries][@agents-md] using [`http.ResponseWriter`][@httpapi-errors] and [`*http.Request`][@httpapi-errors]. This ensures compatibility with streaming responses, JSONL output, multipart uploads, and SDK behaviors. Request parsing utilities in [`internal/httpapi/handler_helpers.go`][@httpapi-helpers] provide common operations like body decoding, metadata normalization, and pagination.

Multi-tenant boundaries are enforced at the handler level using organization and workspace IDs from authenticated principals. All database queries include these scope identifiers to prevent cross-tenant data access.
