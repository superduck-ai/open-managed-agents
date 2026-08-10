---
title: Coverage Map
summary: Frozen page inventory for this first wiki build.
topics: [build, wiki, reference]
sources: []
---

## Page Inventory

This coverage map organizes the planned wiki pages for the Open Managed Agents repository. The system is a monolithic Go backend with React/Vite frontend implementing the Anthropic Managed Agents API with Claude Code runtime integration.

### Getting Started

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/getting-started.md` | getting-started | Front door to the wiki, explaining project purpose and navigation | Links to all major concepts and architecture pages | `README.md`, `main.go` |

### Concepts

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/concepts/managed-agents.md` | managed-agents | What managed agents are and how they differ from standard API usage | Links to [sessions architecture](../architecture/sessions), [skills runtime](../architecture/skills-runtime), [permission policies](../architecture/permission-policies) | `internal/agents/`, `internal/sessions/` |
| `almanac/concepts/claude-code-runtime.md` | claude-code-runtime | CCR v2 integration, epoch-based ownership, and worker lifecycle | Links to [code sessions architecture](../architecture/code-sessions), [worker events](../architecture/worker-events), [permission bridge](../architecture/permission-bridge) | `internal/codesessions/`, `docs/design/be/ccrv2/` |
| `almanac/concepts/skills-system.md` | skills-system | Built-in vs custom skills, versioning, and resolution | Links to [skills runtime](../architecture/skills-runtime), [skill prewarm](../architecture/skill-prewarm) | `internal/skills/`, `skills/examples/` |
| `almanac/concepts/permission-policies.md | permission-policies | Tool permission policies (`always_allow`, `always_ask`) and how they map to Claude Code | Links to [permission bridge](../architecture/permission-bridge), [auth boundaries](../architecture/auth-boundaries) | `docs/design/be/permission-policies.md`, `docs/design/be/managed-agent-claude-code-permission-bridge.md` |
| `almanac/concepts/mcp-integration.md` | mcp-integration | Model Context Protocol server integration and configuration | Links to [environments architecture](../architecture/environments), [sessions architecture](../architecture/sessions) | `internal/platform/mcp_tunnels.go` |
| `almanac/concepts/credential-routing.md` | credential-routing | How authentication credentials determine request routing (API key vs session cookie) | Links to [API architecture](../architecture/api), [auth boundaries](../architecture/auth-boundaries) | `docs/design/be/auth-credential-routing.md` |
| `almanac/concepts/platform-vs-service-api.md` | platform-vs-service-api | Distinction between platform console API and Anthropic-compatible service API | Links to [API architecture](../architecture/api), [workbench architecture](../architecture/workbench) | `internal/api/server.go` |
| `almanac/concepts/builtin-skills.md` | builtin-skills | How built-in skills from Anthropic are seeded and versioned | Links to [skills runtime](../architecture/skills-runtime), [skills system](../concepts/skills-system) | `internal/skills/seed.go`, `docs/design/be/skills-builtin-seed-and-storage.md` |

### Architecture

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/architecture/overview.md` | overview | High-level system architecture, monolith structure, and major boundaries | Links to all architecture pages | `AGENTS.md`, `internal/api/server.go` |
| `almanac/architecture/api.md` | api | HTTP routing, middleware, resource mounting, and entrypoint organization | Links to [platform API](../architecture/platform-api), [auth middleware](../architecture/auth-middleware) | `internal/api/server.go`, `internal/httpapi/` |
| `almanac/architecture/sessions.md` | sessions | Session lifecycle, event streaming, and SSE implementation | Links to [managed agents](../concepts/managed-agents), [code sessions](../architecture/code-sessions), [worker events](../architecture/worker-events) | `internal/sessions/` |
| `almanac/architecture/code-sessions.md` | code-sessions | Claude Code runtime session management, worker ingress, and bridge authentication | Links to [claude-code-runtime](../concepts/claude-code-runtime), [worker events](../architecture/worker-events) | `internal/codesessions/` |
| `almanac/architecture/worker-events.md` | worker-events | Worker event delivery, ACK protocol, and state machine | Links to [claude-code-runtime](../concepts/claude-code-runtime), [code sessions](../architecture/code-sessions) | `docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md` |
| `almanac/architecture/skills-runtime.md` | skills-runtime | Skill resolution, manifest generation, volume mounting, and caching | Links to [skills system](../concepts/skills-system), [skill prewarm](../architecture/skill-prewarm) | `internal/skills/resolver.go`, `docs/design/be/managed-agent-skills-runtime.md` |
| `almanac/architecture/skill-prewarm.md` | skill-prewarm | Async skill volume prewarm jobs and fanout on version changes | Links to [skills runtime](../architecture/skills-runtime), [environments](../architecture/environments) | `internal/skillprewarm/` |
| `almanac/architecture/environments.md` | environments | Sandbox/environment management, E2B integration, and network policies | Links to [skills runtime](../architecture/skills-runtime), [sessions](../architecture/sessions) | `internal/environments/` |
| `almanac/architecture/permission-bridge.md` | permission-bridge | Mapping managed agent permission policies to Claude Code runtime permissions | Links to [permission policies](../concepts/permission-policies), [claude-code-runtime](../concepts/claude-code-runtime) | `docs/design/be/managed-agent-claude-code-permission-bridge.md` |
| `almanac/architecture/auth-boundaries.md` | auth-boundaries | Authentication boundaries, credential routing, and platform vs service auth | Links to [credential routing](../concepts/credential-routing), [API architecture](../architecture/api) | `internal/auth/`, `docs/design/be/db-platform-auth-boundaries.md` |
| `almanac/architecture/platform-api.md` | platform-api | Platform console API routes, organization/workspace management, and admin functions | Links to [API architecture](../architecture/api), [workbench](../architecture/workbench) | `internal/platformapi/` |
| `almanac/architecture/workbench.md` | workbench | Workbench HTTP routes and session streaming for the platform console | Links to [platform-api](../architecture/platform-api), [sessions](../architecture/sessions) | `internal/workbench/` |
| `almanac/architecture/database.md` | database | Database schema, migration patterns, and query organization | Links to [overview](../architecture/overview), [persistence patterns](../architecture/persistence) | `internal/db/`, `internal/db/migrations/` |
| `almanac/architecture/persistence.md` | persistence | Persistence layer patterns, transaction handling, and data access | Links to [database](../architecture/database), [storage](../architecture/storage) | `internal/db/`, `internal/storage/` |
| `almanac/architecture/storage.md` | storage | Object storage (MinIO), file storage, and cleanup workers | Links to [persistence](../architecture/persistence), [files API](../architecture/files-api) | `internal/storage/`, `internal/files/` |
| `almanac/architecture/files-api.md` | files-api | Files API implementation, upload handling, and workspace scoping | Links to [API architecture](../architecture/api), [storage](../architecture/storage) | `internal/files/` |
| `almanac/architecture/deployments.md` | deployments | Deployment API, deployment runs, and batch processing | Links to [agents architecture](../architecture/agents), [batches](../architecture/batches) | `internal/deployments/` |
| `almanac/architecture/batches.md` | batches | Message batch processing and batch worker implementation | Links to [deployments](../architecture/deployments), [database](../architecture/database) | `internal/batches/` |
| `almanac/architecture/webhooks.md` | webhooks | Webhook endpoints, event delivery, and retry logic | Links to [sessions](../architecture/sessions), [database](../architecture/database) | `internal/webhooks/` |
| `almanac/architecture/agents.md` | agents | Agents API, agent versions, and agent snapshots | Links to [deployments](../architecture/deployments), [skills runtime](../architecture/skills-runtime) | `internal/agents/` |
| `almanac/architecture/memory-stores.md` | memory-stores | Memory stores API and memory versioning | Links to [sessions](../architecture/sessions), [database](../architecture/database) | `internal/memory/` |
| `almanac/architecture/vaults.md` | vaults | Vaults API, credential storage, and MCP OAuth flows | Links to [mcp-integration](../concepts/mcp-integration), [platform-api](../architecture/platform-api) | `internal/vaults/` |

### Frontend Architecture

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/architecture/frontend.md` | frontend | Frontend architecture, feature-sliced organization, and tech stack | Links to [frontend components](../architecture/frontend-components), [routing](../architecture/frontend-routing) | `web/AGENTS.md`, `web/src/` |
| `almanac/architecture/frontend-components.md` | frontend-components | Component organization, shadcn/ui usage, and shared UI | Links to [frontend](../architecture/frontend), [frontend routing](../architecture/frontend-routing) | `web/src/shared/`, `web/src/features/` |
| `almanac/architecture/frontend-routing.md` | frontend-routing | TanStack Router setup, route guards, and navigation | Links to [frontend](../architecture/frontend), [auth boundaries](../architecture/auth-boundaries) | `web/src/app/` |
| `almanac/architecture/frontend-state.md` | frontend-state | Server state management with TanStack Query and local state patterns | Links to [frontend](../architecture/frontend), [platform-api](../architecture/platform-api) | `web/src/features/` |
| `almanac/architecture/session-ui.md` | session-ui | Session detail UI, timeline visualization, and tool call display | Links to [sessions](../architecture/sessions), [frontend components](../architecture/frontend-components) | `docs/design/fe/sessions/` |
| `almanac/architecture/managed-agents-ui.md` | managed-agents-ui | Managed agents console UI, agent creation, and skills selection | Links to [agents](../architecture/agents), [skills system](../concepts/skills-system) | `web/src/features/managed-agents/` |
| `almanac/architecture/workbench-ui.md` | workbench-ui | Workbench UI implementation and session streaming | Links to [workbench](../architecture/workbench), [session-ui](../architecture/session-ui) | `web/src/features/workbench/` |

### Decisions

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/decisions/monolithic-architecture.md` | monolithic-architecture | Decision to use monolithic architecture with clear boundaries | Links to [overview](../architecture/overview), [API architecture](../architecture/api) | `AGENTS.md` |
| `almanac/decisions/no-foreign-keys.md` | no-foreign-keys | Decision to avoid database foreign keys and use application-layer integrity | Links to [database](../architecture/database), [persistence](../architecture/persistence) | `AGENTS.md`, `internal/db/migrations/00001_init.sql` |
| `almanac/decisions/credential-based-routing.md` | credential-based-routing | Decision to route based on credentials instead of Host headers | Links to [credential routing](../concepts/credential-routing), [API architecture](../architecture/api) | `docs/design/be/auth-credential-routing.md` |
| `almanac/decisions/epoch-based-ownership.md` | epoch-based-ownership | Decision to use epoch-based worker ownership instead of heartbeat-implicit writes | Links to [claude-code-runtime](../concepts/claude-code-runtime), [code sessions](../architecture/code-sessions) | `docs/design/be/ccrv2/ccr-v2-epoch-design.md` |
| `almanac/decisions/shadcn-ui-frontend.md` | shadcn-ui-frontend | Decision to use shadcn/ui with feature-sliced architecture for the frontend | Links to [frontend](../architecture/frontend), [frontend components](../architecture/frontend-components) | `web/AGENTS.md`, `web/design/` |
| `almanac/decisions/skill-volume-caching.md` | skill-volume-caching | Decision to use deterministic manifest hash for skill volume caching | Links to [skills runtime](../architecture/skills-runtime), [skill prewarm](../architecture/skill-prewarm) | `docs/design/be/managed-agent-skills-runtime.md` |
| `almanac/decisions/application-layer-permissions.md` | application-layer-permissions | Decision to handle permissions at application layer rather than database layer | Links to [permission-bridge](../architecture/permission-bridge), [auth boundaries](../architecture/auth-boundaries) | `docs/design/be/db-platform-auth-boundaries.md` |
| `almanac/decisions/goose-migrations.md` | goose-migrations | Decision to use goose for database migrations and schema management | Links to [database](../architecture/database), [no-foreign-keys](../decisions/no-foreign-keys) | `internal/db/migrations/` |
| `almanac/decisions/static-frontend.md` | static-frontend | Decision to use static Vite build instead of Next.js or server-side rendering | Links to [frontend](../architecture/frontend), [API architecture](../architecture/api) | `web/AGENTS.md` |
| `almanac/decisions/event-delivery-ack.md` | event-delivery-ack | Decision to use application-layer ACK for worker event delivery | Links to [worker-events](../architecture/worker-events), [code sessions](../architecture/code-sessions) | `docs/design/be/ccrv2/ccr-v2-api-worker-events-delivery.md` |

### Guides

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/guides/local-development.md` | local-development | Setting up local development environment, running servers, and common workflows | Links to [overview](../architecture/overview), [testing](../guides/testing) | `justfile`, `.env.example`, `docker-compose.yml` |
| `almanac/guides/testing.md` | testing | How to run tests at different levels (unit, integration, E2E) | Links to [local-development](../guides/local-development), [database](../architecture/database) | `tests/`, `AGENTS.md` |
| `almanac/guides/adding-api-endpoint.md` | adding-api-endpoint | How to add a new API endpoint following project conventions | Links to [API architecture](../architecture/api), [httpapi](../reference/httpapi) | `AGENTS.md`, `internal/api/` |
| `almanac/guides/adding-database-migration.md` | adding-database-migration | How to add a database migration using goose | Links to [database](../architecture/database), [goose-migrations](../decisions/goose-migrations) | `internal/db/migrations/`, `cmd/migrate/` |
| `almanac/guides/creating-custom-skill.md` | creating-custom-skill | How to create and test a custom skill | Links to [skills system](../concepts/skills-system), [skills runtime](../architecture/skills-runtime) | `skills/examples/`, `internal/skills/handler.go` |
| `almanac/guides/seeding-builtin-skills.md` | seeding-builtin-skills | How to seed built-in skills from Anthropic into the database | Links to [builtin-skills](../concepts/builtin-skills), [database](../architecture/database) | `cmd/seed-builtin-skills/`, `internal/skills/seed.go` |
| `almanac/guides/debugging-sessions.md` | debugging-sessions | How to debug session issues, event delivery, and worker problems | Links to [sessions](../architecture/sessions), [worker-events](../architecture/worker-events) | `tests/sessions_api_test.go`, `internal/codesessions/` |
| `almanac/guides/adding-frontend-feature.md` | adding-frontend-feature | How to add a new frontend feature following project conventions | Links to [frontend](../architecture/frontend), [frontend components](../architecture/frontend-components) | `web/AGENTS.md`, `web/src/features/` |
| `almanac/guides/deploying.md` | deploying | How to deploy the system using docker-compose or other methods | Links to [overview](../architecture/overview), [local-development](../guides/local-development) | `docker-compose.yml`, `Dockerfile` |
| `almanac/guides/configuring-mcp-server.md` | configuring-mcp-server | How to configure and use MCP servers with managed agents | Links to [mcp-integration](../concepts/mcp-integration), [vaults](../architecture/vaults) | `docs/design/be/`, `internal/platform/` |
| `almanac/guides/worker-restart-and-recovery.md` | worker-restart-and-recovery | How worker restart, epoch recovery, and state restoration work | Links to [claude-code-runtime](../concepts/claude-code-runtime), [code sessions](../architecture/code-sessions) | `docs/design/be/ccrv2/worker-get-api.md` |

### Reference

| Path | Slug | Purpose | Links | Key Files |
|------|------|---------|-------|-----------|
| `almanac/reference/api-routes.md` | api-routes | Complete reference of all API routes across platform and service APIs | Links to [API architecture](../architecture/api), [platform-api](../architecture/platform-api) | `internal/api/server.go` |
| `almanac/reference/database-schema.md` | database-schema | Complete database schema reference with all tables and relationships | Links to [database](../architecture/database), [goose-migrations](../decisions/goose-migrations) | `internal/db/migrations/00001_init.sql` |
| `almanac/reference/httpapi.md` | httpapi | HTTP API utilities, error handling, and response formatting | Links to [API architecture](../architecture/api), [adding-api-endpoint](../guides/adding-api-endpoint) | `internal/httpapi/` |
| `almanac/reference/environment-variables.md` | environment-variables | All environment variables and configuration options | Links to [local-development](../guides/local-development), [config package](../reference/config-package) | `.env.example`, `internal/config/` |
| `almanac/reference/config-package.md` | config-package | Configuration package structure and loading logic | Links to [environment-variables](../reference/environment-variables), [overview](../architecture/overview) | `internal/config/` |
| `almanac/reference/external-ids.md` | external-ids | External ID format and generation rules for API compatibility | Links to [API architecture](../architecture/api), [database-schema](../reference/database-schema) | `internal/ids/` |
| `almanac/reference/errors.md` | errors | Error types, error handling patterns, and Anthropic-compatible error responses | Links to [httpapi](../reference/httpapi), [API architecture](../architecture/api) | `internal/httpapi/` |
| `almanac/reference/agent-snapshot.md` | agent-snapshot | Agent snapshot structure, skills references, and versioning | Links to [agents](../architecture/agents), [skills system](../concepts/skills-system) | `internal/agents/`, `internal/agentsnapshot/` |
| `almanac/reference/session-events.md` | session-events | Session event types, event payload structures, and streaming format | Links to [sessions](../architecture/sessions), [worker-events](../architecture/worker-events) | `internal/sessions/event_payload.go` |
| `almanac/reference/worker-api.md` | worker-api | Worker API endpoints for state management, heartbeats, and event delivery | Links to [code-sessions](../architecture/code-sessions), [worker-events](../architecture/worker-events) | `docs/design/be/ccrv2/ccr-v2-api-worker-state.md` |
| `almanac/reference/permission-modes.md` | permission-modes | Claude Code permission modes and how they map to managed agent policies | Links to [permission-policies](../concepts/permission-policies), [permission-bridge](../architecture/permission-bridge) | `docs/design/be/ccrv2/claude_code-permission-modes.md` |
| `almanac/reference/skill-manifest.md` | skill-manifest | Skill manifest structure, hash generation, and volume naming | Links to [skills runtime](../architecture/skills-runtime), [skill-volume-caching](../decisions/skill-volume-caching) | `internal/skills/mount_manifest.go` |
| `almanac/reference/auth-tokens.md` | auth-tokens | Authentication token formats, API keys, and session cookies | Links to [auth-boundaries](../architecture/auth-boundaries), [credential-routing](../concepts/credential-routing) | `internal/auth/`, `internal/platformauth/` |
| `almanac/reference/testing-helpers.md` | testing-helpers | Testing utilities, fixtures, and test helpers | Links to [testing](../guides/testing), [database](../architecture/database) | `tests/`, `internal/sessions/fixtures.go` |

## Organization Notes

### Page Type Distribution
- **Concepts (8 pages)**: Core mental models and domain concepts
- **Architecture (25 pages)**: System areas, components, and technical boundaries  
- **Decisions (10 pages)**: Architecturally significant choices and tradeoffs
- **Guides (12 pages)**: Task-oriented how-to instructions
- **Reference (14 pages)**: Lookup material for contracts and formats

### Major Topic Clusters
- **Managed Agents Core**: Concepts, architecture, and guides for managed agents, sessions, and Claude Code runtime
- **Skills System**: Skills concepts, runtime, prewarm, and development guides
- **Authentication & Authorization**: Credential routing, auth boundaries, and permission policies
- **API & HTTP**: API architecture, routing, endpoints, and error handling
- **Database & Persistence**: Schema, migrations, persistence patterns, and transactions
- **Frontend**: Frontend architecture, components, routing, state, and UI patterns
- **Platform & Console**: Platform API, workbench, and console features

### Cross-Cutting Concerns
- **Multi-tenancy**: Organization/workspace boundaries appear across database, API, and auth pages
- **API Compatibility**: Anthropic API compatibility is a constraint across API, sessions, and agents pages
- **Event Streaming**: SSE and event delivery connect sessions, workers, and frontend
- **External Services**: E2B, MinIO, Redis, and PostgreSQL integration patterns
