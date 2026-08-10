---
title: "Deploying"
summary: "Deploy Open Managed Agents using Docker Compose for local development or production environments."
topics: [deployment, operations]
sources:
  - id: docker-compose-yml
    type: file
    path: docker-compose.yml
  - id: deployment-design-md
    type: file
    path: docs/design/docker-compose-deployment.md
  - id: env-config
    type: file
    path: .env.example
---

Open Managed Agents deploys as a Docker Compose stack, providing a complete runtime environment with PostgreSQL, Redis, MinIO, and E2B sandbox support. The stack can run entirely on a single machine using Linux Docker Engine 20.10+ or OrbStack on macOS.

## Architecture

The stack consists of six services:

- **caddy** (`:28080`) — Serves the frontend SPA and proxies API requests
- **oma-server** (`:38080`) — Main API server handling `/api` and `/v1` endpoints
- **e2b-local** (`:3099`) — Sandbox gateway managing Claude Code execution environments
- **postgres** (`:5432`) — PostgreSQL 17 for metadata storage
- **redis** (`:6379`) — Redis 8 for platform session storage
- **minio** (`:9000/9001`) — S3-compatible object storage for files and skills[@docker-compose-yml]

Service dependencies flow from Caddy through oma-server to the data stores and e2b-local. The oma-server communicates with e2b-local via `host.docker.internal:3099`[@deployment-design-md].

## Prerequisites

Deployment requires Linux Docker Engine 20.10+ or OrbStack on macOS. Docker Desktop for Mac/Windows does not support the `network_mode: host` required by e2b-local[@docker-compose-yml].

Before starting, pull the sandbox template image:

```bash
docker pull ghcr.io/superduck-ai/claude-code-interpreter:latest
```

Configure the upstream Anthropic API key in `.env`:

```bash
ANTHROPIC_UPSTREAM_API_KEY=sk-ant-...
ANTHROPIC_UPSTREAM_BASE_URL=https://api.anthropic.com
```

## Starting the Stack

Launch all services with:

```bash
docker compose up -d
```

Access endpoints:

| Service | URL |
|---------|-----|
| Console frontend | `http://localhost:28080` |
| oma API | `http://localhost:38080` |
| e2b-local | `http://localhost:3099` |
| MinIO Web UI | `http://localhost:9001` |

If port 28080 is occupied, set `CADDY_HOST_PORT` in `.env` or use `CADDY_HOST_PORT=0` for automatic port assignment[@docker-compose-yml].

Stop and cleanup:

```bash
docker compose down        # Stop services
docker compose down -v    # Stop and delete volumes
```

## Infrastructure Services

PostgreSQL stores all metadata with health-checked startup and automatic migration. Redis provides session caching with persistence. MinIO handles S3-compatible storage with configurable credentials[@docker-compose-yml].

Data persistence uses Docker named volumes—`pgdata`, `redisdata`, and `miniodata`—preserving data across restarts. For host-based storage, replace volume references with bind mounts.

## e2b-local Networking

The e2b-local service uses `network_mode: host` to access sandbox containers running on the host Docker daemon. This allows direct access to dynamically mapped sandbox ports like `127.0.0.1:32768`[@deployment-design-md].

The oma-server reaches e2b-local via `host.docker.internal`, enabled by `extra_hosts` configuration. The e2b-local configuration binds to `0.0.0.0:3099` to accept connections from the compose bridge network[@deployment-design-md].

## Frontend Delivery

The init-web container extracts static assets from the oma-server image into a shared volume. Caddy then serves these files with SPA fallback and API proxying[@docker-compose-yml]. This multi-stage approach separates asset extraction from runtime serving.

## Environment Configuration

Key environment variables for oma-server are documented in `.env.example` [@env-config]:

- `DATABASE_URL` — PostgreSQL connection string
- `REDIS_URL` — Redis connection string
- `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY` — MinIO credentials
- `E2B_API_URL`, `E2B_SANDBOX_URL` — E2B service endpoints
- `ANTHROPIC_UPSTREAM_API_KEY` — Anthropic API credential

Caddy port customization uses `CADDY_HOST_PORT`, defaulting to 28080[@docker-compose-yml].

## Local Development Mode

For local development with hot-reload, run only the infrastructure services:

```bash
docker compose up -d postgres redis minio e2b-local
```

Then start the backend with `go run .` and the frontend with `bun run dev` from the `web/` directory. The Vite dev server proxies API requests to the local backend[@deployment-design-md].

## Service Health

All infrastructure services include health checks. Postgres waits for `pg_isready`, Redis responds to `PING`, and MinIO confirms readiness via `mc ready`[@docker-compose-yml]. The oma-server health endpoint is `/healthz`[@docker-compose-yml].
