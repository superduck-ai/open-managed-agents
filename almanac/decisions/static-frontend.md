---
title: "Static Frontend"
summary: "The console UI is a static Vite application served by the Go backend, not a separate Node.js server."
topics: [architecture, frontend]
sources:
  - id: web-agents
    type: file
    path: web/AGENTS.md
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: vite-config
    type: file
    path: web/vite.config.ts
---

The Open Managed Agents console UI is a static Vite + React + TypeScript application built in the `web/` directory and served by the Go backend. Production deployment serves pre-built static files from `web/dist/` via Nginx or another static file server.

## Build and Runtime

Frontend development uses Bun as the package manager and runtime:

* `bun install` — Install dependencies
* `bun run dev` — Start Vite dev server with API proxying
* `bun run build` — Produce static files in `web/dist/`
* `bun test` — Run unit tests [@web-agents]

The Vite development server proxies `/api` and `/v1` requests to the Go backend (typically `127.0.0.1:38080`), allowing frontend development against a live API without CORS complications [@vite-config].

## Production Serving

Production does not run a Node.js or Bun HTTP server for the frontend. The `bun run build` command generates static files that:

* The Go backend serves directly
* Nginx serves in recommended deployments
* Contain no server-side rendering or server-side logic

This architecture treats the frontend as a pure static client application [@web-agents] [@agents-md].

## API Boundaries

The frontend maintains strict separation between two API surfaces:

* `/api/*` — Console API for platform operations (authentication, workspace management, member administration)
* `/v1/*` — Anthropic-compatible API for agents, sessions, and SDK interactions

Frontend API clients in `src/shared/api/` distinguish between these boundaries. The `/api` client requires cookies and CSRF tokens for stateful operations, while the `/v1` client uses API keys for SDK-compatible requests [@web-agents].

## Authentication Model

Session-based authentication uses cookies rather than localStorage or sessionStorage tokens:

* `POST /api/auth/login` — Authenticate and receive session cookie
* `GET /api/bootstrap` — Load account, organization, workspace, permissions, and CSRF token into application state
* Cookie-based session identity — No manual token management in frontend code
* `X-CSRF-Token` header — Required for all cookie-authenticated write requests

This model requires the Go backend to serve the frontend; the static files cannot be hosted on a separate domain without CORS and cookie complications [@web-agents].

## Tech Stack Choices

The frontend uses React-based tooling prioritized for developer experience and ecosystem alignment:

* **TanStack Router** — File-based routing with route guards for protected pages
* **TanStack Query** — Server state management and caching
* **shadcn/ui (new-york)** — Component library providing pre-built UI primitives
* **Tailwind CSS** — Styling with semantic design tokens
* **Bun** — Package management and test runtime

Explicitly excluded are Next.js, Remix, and server-side frameworks—the frontend is pure client-side code [@web-agents].

## Role-Based UI

Permission-aware UI elements use bootstrap-provided role data from `/api/bootstrap`:

* `user` — Can use Workbench
* `claude_code_user` — Can use Workbench and Claude Code
* `developer` — Can use Workbench, Claude Code, and manage API keys
* `billing` — Can use Workbench and manage billing
* `admin` — Can perform all actions including user management

Frontend permission checks are UX conveniences only. Backend RBAC remains the authoritative source [@web-agents].

## Development Workflow

Local development restarts use project root scripts:

* `./restart-web.sh` — Stop and restart the Vite dev server
* `./scripts/restart-server.sh` — Restart the Go backend

Modified frontend code requires a Vite server restart before browser verification. The Go backend and Vite dev server run as separate processes during development [@agents-md].
