---
title: "Credential Routing"
summary: "Request routing based on authentication credentials rather than host headers."
topics: [authentication, routing, api]
sources:
  - id: auth-design
    type: file
    path: docs/design/be/auth-credential-routing.md
  - id: auth-extract
    type: file
    path: internal/auth/auth.go
  - id: api-server
    type: file
    path: internal/api/server.go
---

The `/v1/*` entrypoint routes requests based on the authentication credentials present in the request rather than the `Host` header. This enables correct routing through reverse proxies and arbitrary port deployments.

## Routing Decision

The `apiEntrypointRouter` examines credentials in order:

1. **API Key Present**: Route to the service API (SDK/CLI calls)
2. **Session Cookie Present**: Route to the platform API (browser console)
3. **No Credentials**: Route to the platform API (default, preserving open routes like `/v1/privacy-consents`) [@api-server]

When both an API key and session cookie are present, the API key takes precedence as it indicates explicit service API usage intent.

## Credential Extraction

Two credential types are extracted from requests:

- **API Key**: From `X-Api-Key` header or `Authorization: Bearer <token>` header via `auth.ExtractAPIKey()` [@auth-extract]
- **Session Cookie**: From the `sessionKey` cookie via `auth.ExtractPlatformSessionKey()` [@auth-extract]

## Authentication Paths

Once routed, requests follow different authentication flows:

**Service API** (`serviceAuthMiddleware`):
- Validates API key against hashed database values
- Supports both workspace API keys and environment-specific keys
- Returns organization, workspace, and optional environment context

**Platform API** (`platformAuthMiddleware`):
- Validates session key against the platform session store (Redis-backed)
- Supports mirror session recovery for organization/workspace context
- Clears invalid session cookies with `HttpOnly: true` and `SameSite: Lax` attributes [@auth-design]

## Security Enhancements

The credential-based routing introduced several security improvements:

- Session cookies now use `HttpOnly: true` and `SameSite: Lax` to prevent CSRF and XSS theft
- Invalid sessions are cleared immediately on authentication failure
- Mirror sessions can be recovered from `lastActiveOrg` cookie or path-based organization ID
- All `isPlatformHost` checks were removed as dead code [@auth-design]

This routing approach enables docker-compose deployments where Caddy serves on `:80` and forwards to the backend, with `Host: localhost` being correctly recognized as a platform request when a session cookie is present.
