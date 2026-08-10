---
title: "API Routes"
summary: "Complete reference of HTTP routes across the Open Managed Agents platform and service APIs."
topics: [api, reference, routing]
sources:
  - id: server-go
    type: file
    path: internal/api/server.go
  - id: agents-md
    type: file
    path: AGENTS.md
---

# API Routes

The Open Managed Agents HTTP server exposes three distinct route surfaces: a health check endpoint, an Anthropic-compatible service API, and a platform console API. Routes are organized in [`internal/api/server.go`][@server-go] using [Go-Chi/Chi v5][@server-go] for routing and middleware.

## Entrypoint Routes

The server registers these top-level routes in [`NewServerWithPlatformSessions()`][@server-go]:

| Path | Handler | Purpose |
|------|---------|---------|
| `/healthz` | Health check | Returns `{"status": "ok"}` for liveness probes |
| `/v1` | v1Entrypoints | Service API and platform API entrypoint |
| `/v1/*` | v1Entrypoints | Service API and platform API with subroutes |
| `/api` | platformConsoleAPI | Platform console API routes |
| `/api/*` | platformConsoleAPI | Platform console API with subroutes |
| `/auth` | platformConsoleAPI | Authentication routes (magic link, OAuth) |
| `/auth/*` | platformConsoleAPI | Authentication with subroutes |
| `/oauth` | platformConsoleAPI | OAuth callback routes |
| `/oauth/*` | platformConsoleAPI | OAuth callbacks with subroutes |
| `/web-api` | platformConsoleAPI | Web API routes |
| `/web-api/*` | platformConsoleAPI | Web API with subroutes |

## v1 Entrypoint Routing

The [`v1EntrypointRouter`][@server-go] implements credential-based routing to direct requests to either the service or platform API:

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

Requests with `X-Api-Key` or `Authorization: Bearer` headers route to the service API. Requests with a `sessionKey` cookie route to the platform API. Missing credentials default to platform routing for unauthenticated operations.

## Shared v1 Resources

Both service and platform APIs mount shared resources via [`mountSharedV1Resources()`][@server-go]:

| Route | Handler | Methods |
|-------|---------|---------|
| `/v1/agents:search` | agents.Search | POST |
| `/v1/agents` | agents | GET, POST, PUT, DELETE |
| `/v1/agents/{agent_id}` | agents | GET, POST, DELETE |
| `/v1/agents/{agent_id}/archive` | agents | POST |
| `/v1/agents/{agent_id}/versions` | agents | GET |
| `/v1/deployment_runs` | deploymentRuns | GET, POST |
| `/v1/deployment_runs/{deployment_run_id}` | deploymentRuns | GET |
| `/v1/deployments` | deployments | GET, POST |
| `/v1/deployments/{deployment_id}` | deployments | GET, POST, DELETE |
| `/v1/deployments/{deployment_id}/archive` | deployments | POST |
| `/v1/environments` | environments | GET, POST |
| `/v1/environments/{environment_id}` | environments | GET, POST, DELETE |
| `/v1/environments/{environment_id}/archive` | environments | POST |
| `/v1/environments/{environment_id}/work` | environments | GET, POST |
| `/v1/environments/{environment_id}/work/{work_id}` | environments | GET, POST, DELETE |
| `/v1/files` | files | GET, POST, DELETE |
| `/v1/files/{file_id}` | files | GET, DELETE |
| `/v1/files/{file_id}/content` | files | GET |
| `/v1/memory_stores` | memory | GET, POST |
| `/v1/memory_stores/{memory_store_id}` | memory | GET, POST, DELETE |
| `/v1/memory_stores/{memory_store_id}/archive` | memory | POST |
| `/v1/memory_stores/{memory_store_id}/memories` | memory | GET, POST |
| `/v1/memory_stores/{memory_store_id}/memories/{memory_id}` | memory | GET, POST, DELETE |
| `/v1/messages/batches` | batches | GET, POST |
| `/v1/messages/batches/{batch_id}` | batches | GET, POST, DELETE |
| `/v1/messages/batches/{batch_id}/results` | batches | GET |
| `/v1/models` | models | GET |
| `/v1/organizations` | admin | GET |
| `/v1/organizations/{organization_id}` | admin | GET, POST |
| `/v1/sessions` | sessions | GET, POST |
| `/v1/sessions/{session_id}` | sessions | GET, POST, DELETE |
| `/v1/sessions/{session_id}/archive` | sessions | POST |
| `/v1/sessions/{session_id}/events` | sessions | GET, POST |
| `/v1/sessions/{session_id}/events/stream` | sessions | GET (SSE) |
| `/v1/sessions/{session_id}/threads` | sessions | GET, POST |
| `/v1/sessions/{session_id}/threads/{thread_id}` | sessions | GET |
| `/v1/sessions/{session_id}/threads/{thread_id}/archive` | sessions | POST |
| `/v1/skills` | skills | GET, POST |
| `/v1/skills/{skill_id}` | skills | GET, POST, DELETE |
| `/v1/skills/{skill_id}/archive` | skills | POST |
| `/v1/skills/{skill_id}/versions` | skills | GET |
| `/v1/skills/{skill_id}/versions/{version}` | skills | GET |
| `/v1/vaults` | vaults | GET, POST |
| `/v1/vaults/{vault_id}` | vaults | GET, POST, DELETE |
| `/v1/vaults/{vault_id}/archive` | vaults | POST |
| `/v1/vaults/{vault_id}/credentials` | vaults | GET, POST |
| `/v1/vaults/{vault_id}/credentials/{credential_id}` | vaults | GET, POST, DELETE |
| `/v1/vaults/{vault_id}/credentials/{credential_id}/archive` | vaults | POST |
| `/v1/webhooks` | webhooks | GET, POST |
| `/v1/webhooks/{webhook_id}` | webhooks | GET, POST, DELETE |
| `/v1/webhooks/{webhook_id}/archive` | webhooks | POST |

## Platform Console API Routes

The platform console API at [`platformConsoleAPIRouter()`][@server-go] provides web UI operations:

### Unauthenticated Routes

| Route | Handler | Purpose |
|-------|---------|---------|
| `/api/privacy-consent` | platformapi | Privacy consent management |
| `/api/directory` | platformapi | Organization directory |
| `/api/account` | platformapi | Account lookup (email) |
| `/api/account/login/email` | platformapi | Email-based login |
| `/api/billing` | platformapi | Billing information |
| `/oauth/vault/success` | server | MCP OAuth success callback |

### Authenticated Organization Routes

| Route | Handler | Purpose |
|-------|---------|---------|
| `/api/organizations/{orgUuid}` | platformapi | Organization root operations |
| `/api/organizations/{orgUuid}/profile` | platformapi | Organization profile |
| `/api/organizations/{orgUuid}/sso` | platformapi | SSO configuration |
| `/api/organizations/{orgUuid}/onboarding` | platformapi | Onboarding status |
| `/api/organizations/{orgUuid}/experience` | platformapi | Experience settings |
| `/api/organizations/{orgUuid}/billing` | platformapi | Billing management |
| `/api/organizations/{orgUuid}/analytics` | platformapi | Analytics data |
| `/api/organizations/{orgUuid}/proxy` | platformapi | Proxy configuration |
| `/api/organizations/{orgUuid}/mcp/vault-auth/start` | server | MCP vault authentication start |
| `/api/oauth/organizations/{orgUuid}/environments` | platformapi | OAuth environment access |

### Console Management Routes

| Route | Handler | Purpose |
|-------|---------|---------|
| `/api/console/organizations/{orgUuid}/workspaces` | platformapi | Workspace management |
| `/api/console/organizations/{orgUuid}/admin-requests` | platformapi | Admin request handling |
| `/api/console/organizations/{orgUuid}/api-keys` | platformapi | API key console operations |
| `/api/console/organizations/{orgUuid}/members` | platformapi | Member management |
| `/api/console/organizations/{orgUuid}/invites` | platformapi | Invitation management |
| `/api/{orgUuid}/files` | files | Platform file operations |
| `/web-api/sessions/{sessionId}/stream` | server | Web session streaming |

### Workbench Routes

| Route | Handler | Purpose |
|-------|---------|---------|
| `/api/console/organizations/{orgUuid}/prompts` | workbenchapi | Prompt management |
| `/api/console/organizations/{orgUuid}/evaluations` | workbenchapi | Evaluation operations |

## Code Session Routes

Code session routes are registered directly in [`NewServerWithPlatformSessions()`][@server-go]:

| Route | Handler | Purpose |
|-------|---------|---------|
| `/code-sessions` | codeSessions | Code session creation |
| `/code-sessions/{codeSessionId}` | codeSessions | Code session retrieval |
| `/code-sessions/{codeSessionId}/worker-api` | codeSessions | Worker state API |
| `/code-sessions/{codeSessionId}/worker-api/events` | codeSessions | Worker event stream |
| `/code-sessions/{codeSessionId}/worker-api/events/{eventUuid}/ack` | codeSessions | Event ACK endpoint |

## Authentication Middleware

### Service Authentication

The [`serviceAuthMiddleware`][@server-go] validates API keys using [`authenticateService()`][@server-go], which supports both regular API keys and environment-specific keys for paths matching [`isEnvironmentCredentialPath()`][@server-go] (like `/v1/environments/*/work` or `/v1/sessions/*`).

### Platform Authentication

The [`platformAuthMiddleware`][@server-go] validates platform sessions using [`authenticatePlatformSession()`][@server-go] with session recovery logic and organization/workspace override capabilities via headers and query parameters.

## Route Conventions

Routes follow [Go-Chi conventions][@agents-md] using chi patterns for path parameters:

- Resource IDs use `/{resource_id}` patterns (e.g., `/{agent_id}`)
- Nested resources use compound paths (e.g., `/agents/{agent_id}/versions`)
- Sub-mounts use `chi.Mount()` for resource-level routers
- Route groups use `chi.Route()` for shared middleware

All v1 routes return Anthropic-compatible error responses via [`httpapi.WriteError()`][@server-go] and include request IDs for tracing.
