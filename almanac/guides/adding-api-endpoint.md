---
title: "Adding API Endpoint"
summary: "Register new API routes using go-chi/chi, implement handlers in resource packages, and maintain Anthropic-compatible error responses."
topics: [api, development, backend]
sources:
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: server-go
    type: file
    path: internal/api/server.go
  - id: files-handler
    type: file
    path: internal/files/handler.go
  - id: skills-handler
    type: file
    path: internal/skills/handler.go
  - id: httpapi
    type: file
    path: internal/httpapi/
---

Adding API endpoints follows the vertical slice pattern: each resource has its own package under `internal/` with a handler that implements `http.Handler`. Routes are registered in `internal/api/server.go` using chi's sub-router and routing group features [@agents-md].

## Route Registration

New API resources are mounted in `mountSharedV1Resources` for both service and platform authentication modes [@server-go]:

```go
func (s *Server) mountSharedV1Resources(r chi.Router) {
    r.Mount("/files", s.files)
    r.Mount("/skills", s.skills)
    // Add your new resource here
    r.Mount("/new_resource", s.newResource)
}
```

For resources with sub-routes (like `{file_id}/content`), use chi patterns within the resource handler rather than manual path splitting [@agents-md].

## Handler Implementation

Create a new file in `internal/{resource}/handler.go` that implements the standard handler pattern, following the same structure as `files/handler.go` and `skills/handler.go` [@files-handler][@skills-handler]:

```go
package newresource

import (
    "net/http"
    "github.com/go-chi/chi/v5"
)

type Handler struct {
    cfg   config.Config
    db    *db.DB
    store storage.ObjectStore
    router chi.Router
}

func NewHandler(cfg config.Config, database *db.DB, store storage.ObjectStore) *Handler {
    h := &Handler{cfg: cfg, db: database, store: store}
    router := chi.NewRouter()
    router.NotFound(notFound)
    router.MethodNotAllowed(notFound)
    router.Post("/", h.create)
    router.Get("/", h.list)
    router.Get("/{resource_id}", h.retrieve)
    h.router = router
    return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    h.router.ServeHTTP(w, r)
}
```

## Authentication and Authorization

Extract the authenticated principal from the request context to get workspace and organization scope. The `httpapi` package provides helpers for context extraction and error responses [@files-handler][@httpapi]:

```go
principal, ok := auth.PrincipalFromContext(r.Context())
if !ok {
    httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
    return
}
```

Multi-tenant queries must always include `organization_id` and `workspace_id` from the principal to prevent cross-tenant data access [@agents-md].

## Response Formatting

Use `httpapi.WriteJSON` for successful responses and `httpapi.WriteError` for errors to maintain Anthropic-compatible error structure [@server-go]:

```go
httpapi.WriteJSON(w, http.StatusOK, responseData)
httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Missing required field"))
```

Never let framework default error responses leak into the `/v1/*` API surface [@agents-md].

## File Uploads

For multipart file uploads, use `http.MaxBytesReader` to enforce size limits before parsing [@files-handler]:

```go
r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxFileBytes+1024*1024)
if err := r.ParseMultipartForm(32 << 20); err != nil {
    // Handle size limit error
}
```

## Streaming Responses

For streaming responses like SSE, keep handlers at the standard `net/http` boundary to maintain explicit control over flushing and chunking [@agents-md]. This approach ensures compatibility with SDK expectations for streamed content.
