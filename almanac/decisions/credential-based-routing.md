---
title: "Credential-Based Routing"
summary: "API entry point routing determines service versus platform paths based on client credentials rather than Host headers."
topics: [architecture, authentication]
sources:
  - id: auth-design
    type: file
    path: docs/design/be/auth-credential-routing.md
  - id: api-server
    type: file
    path: internal/api/server.go
---

The `/v1/*` entry point routes requests to either the service router or the platform router based on the authentication credentials the client presents. This credential-based routing replaces previous Host header detection, enabling correct routing through reverse proxies and arbitrary port deployments.

## Routing Decision

The `apiEntrypointRouter` examines incoming requests for authentication indicators. If the request contains an API key via the `X-Api-Key` header or `Authorization: Bearer` token, the request routes to the service handler for SDK and CLI calls with token authentication [@api-server].

If the request contains a `sessionKey` cookie but no API key, the request routes to the platform handler for browser console operations with session authentication. When both credentials are present, the API key takes precedence as it indicates explicit service call intent. When neither credential is present, the request defaults to the platform router to preserve open routes such as `/v1/privacy-consents` [@auth-design].

## Implementation

The routing logic in `internal/api/server.go` uses credential extraction functions from the `internal/auth` package. `ExtractAPIKey()` checks for `X-Api-Key` headers or `Authorization: Bearer` tokens. `ExtractPlatformSessionKey()` checks for the `sessionKey` cookie [@api-server].

```go
func (r apiEntrypointRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
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

## Problem Solved

Previous routing used `isPlatformHost()` to whitelist specific hosts such as `localhost:5173` for the Vite dev server and `oma.duck.ai` for production. Requests from any other host, including `localhost`, `localhost:38080`, or reverse proxy domains, were incorrectly routed to the service path requiring API keys [@auth-design].

This caused 401 errors in docker-compose deployments where Caddy serves on port `:80` and the frontend accesses the API via `localhost`. Session cookies were present but requests were routed to the service authentication middleware [@auth-design].

## Security Hardening

The credential routing change required session cookie security improvements. The `sessionKey` cookie now includes `HttpOnly: true` and `SameSite: Lax` attributes to protect against CSRF and XSS theft across arbitrary hosts [@auth-design].

Dead code was removed including the `isPlatformHost()` function and its dependencies. Authentication middleware no longer checks host names before clearing invalid sessions or recovering mirror sessions [@auth-design].

## Compatibility

The change maintains backward compatibility for existing scenarios. API key requests work on any host. Session cookie requests work on any host. The only semantic shift is that session cookies now function correctly regardless of port or domain, which is the intended fix [@auth-design].
