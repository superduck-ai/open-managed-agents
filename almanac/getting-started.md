---
title: "Getting Started"
summary: "Open Managed Agents is a local-first Managed Agents API service that provides an Anthropic SDK-compatible /v1 API surface with a React console."
topics: [getting-started, development-environment]
sources:
  - id: readme-cn
    type: file
    path: README.cn.md
  - id: agents-md
    type: file
    path: AGENTS.md
  - id: main-go
    type: file
    path: main.go
  - id: api-server
    type: file
    path: internal/api/server.go
---

Open Managed Agents is a Go-based local-first service that implements the Managed Agents API with Anthropic SDK compatibility. It provides a `/v1` API surface for programmatic access and a React-based console for interactive management. The service runs as a monolith, combining agents, sessions, environments, files, memory stores, skills, deployments, message batches, vaults, and webhooks into a single server for local development and SDK compatibility verification [@readme-cn].

## Quick Start with Docker Compose

The fastest way to run Open Managed Agents is using Docker Compose, which starts PostgreSQL, Redis, MinIO, an E2B sandbox gateway, the API server, and a Caddy reverse proxy for the frontend [@readme-cn]:

```bash
docker compose up -d
```

After startup, the frontend is available at `http://localhost:28080` and the API at `http://localhost:38080`. The platform requires Linux Docker Engine 20.10+ or OrbStack on macOS.

## Local Development Setup

For local development, you'll need Go (matching `go.mod`), Bun for the frontend, PostgreSQL, Redis, and MinIO or S3-compatible storage [@readme-cn]. The default local configuration expects:

- PostgreSQL at `postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable`
- Redis at `redis://localhost:6379`
- MinIO at `http://localhost:9000` (bucket: `claude-files`, credentials: `minioadmin/minioadmin`)

Configuration is loaded from a `.env` file found by searching upward from the current directory until `go.mod` is reached [@readme-cn]. Development mode automatically enables database migrations.

## Starting the Backend

Run the server from the repository root using the provided script, which handles port cleanup and runs in the foreground [@readme-cn]:

```bash
./scripts/restart-server.sh
```

The server binds to `127.0.0.1:38080` by default. Verify health with:

```bash
curl http://127.0.0.1:38080/healthz
```

## Starting the Frontend

Install dependencies and start the Vite development server [@readme-cn]:

```bash
cd web
bun install
cd ..
./scripts/restart-web.sh
```

The frontend runs at `http://127.0.0.1:5173` and proxies `/api` and `/v1` to the backend.

## API Authentication

The default development API key is `sk-ant-local-default` [@readme-cn]. Test the API with:

```bash
curl http://127.0.0.1:38080/v1/models \
  -H 'Authorization: Bearer sk-ant-local-default'
```

## Running Tests

Execute the full test suite with [@readme-cn]:

```bash
go test ./... -count=1
```

For end-to-end tests with the Go SDK:

```bash
TEST_API_BASE_URL=http://127.0.0.1:38080 \
  go test ./tests -run TestGoSDKFilesE2E -count=1 -v
```

## Architecture Overview

The service follows a clean dependency flow where `internal/api` handles service assembly and routing, resource packages like `internal/{agents,sessions,files}` contain handlers and business logic, and `internal/db` serves as the persistence boundary without depending on API layers [@agents-md]. The server initializes in `main.go` by loading configuration, connecting to databases and stores, starting background workers, and launching the HTTP server [@main-go].

HTTP routing uses `github.com/go-chi/chi/v5` with sub-routers and route groups for resource mounting. Request ID injection, panic recovery, and `/v1/*` authentication happen in API-level middleware. Error responses maintain Anthropic compatibility via `internal/httpapi.WriteError` [@api-server].
