---
title: "Platform vs Service API"
summary: "Distinction between the platform console API and the Anthropic-compatible service API."
topics: [api, architecture, platform]
sources:
  - id: api-server
    type: file
    path: internal/api/server.go
  - id: auth-routing
    type: file
    path: docs/design/be/auth-credential-routing.md
---

The API server exposes two distinct API surfaces that share resource routes but differ in authentication and client purpose: the **platform console API** and the **service API**.

## Route Structure

Both APIs serve `/v1/*` paths but are mounted separately:

- **Platform Console API**: `/api/*`, `/auth/*`, `/oauth/*`, `/web-api/*` - Platform-specific routes for organizations, users, console workspaces, and OAuth flows
- **Service API**: `/v1/*` - Anthropic-compatible API for agents, sessions, files, deployments, and other resources [@api-server]

The `/v1/*` shared resources (agents, sessions, files, skills, vaults, etc.) are mounted under both routers but use different authentication middleware based on credential routing.

## Authentication Differences

| Aspect | Service API | Platform API |
|--------|-------------|--------------|
| **Credential** | API key (`X-Api-Key` or `Authorization: Bearer`) | Session cookie (`sessionKey`) |
| **Principal Source** | API keys table, environment keys table | Platform session store (Redis) |
| **Context Injection** | Organization/workspace from API key | Organization/workspace from session, with mirror session support |
| **Entry Point** | `/v1` router with `serviceAuthMiddleware` | `/v1` router with `platformAuthMiddleware` |

The routing decision happens at the `/v1/*` entrypoint based on which credential is present, not the `Host` header [@auth-routing].

## Platform-Specific Routes

The platform console API includes routes that don't exist in the service API:

- Organization management (`/api/organizations/{orgUuid}`)
- Console workspace administration (`/api/console/organizations/{orgUuid}/workspaces`)
- User invitations and membership (`/api/console/organizations/{orgUuid}/invites`, `/members`)
- API key management for the console (`/api/console/organizations/{orgUuid}/api-keys`)
- MCP vault OAuth initiation (`/api/organizations/{orgUuid}/mcp/vault-auth/start`) [@api-server]

## Shared Resources

The following `/v1/*` resources are available to both APIs but authenticated differently:

- `/v1/agents` - Agent definitions and versions
- `/v1/sessions` - Managed agent sessions
- `/v1/files` - File upload and retrieval
- `/v1/skills` - Skill catalog (both built-in and custom)
- `/v1/vaults` - MCP vaults and credentials
- `/v1/deployments` - Agent deployments
- `/v1/environments` - Environment configurations
- `/v1/memory_stores` - Memory stores
- `/v1/webhooks` - Webhook configurations [@api-server]

All shared resources require `beta=true` query parameter for beta features like vaults.
