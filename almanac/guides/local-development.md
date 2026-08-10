---
title: "Local Development"
summary: "Run the Open Managed Agents platform locally with Docker Compose or manual Go backend and Vite frontend."
topics: [development, setup]
sources:
  - id: justfile
    type: file
    path: justfile
  - id: docker-compose
    type: file
    path: docker-compose.yml
  - id: restart-server
    type: file
    path: scripts/restart-server.sh
  - id: restart-web
    type: file
    path: scripts/restart-web.sh
  - id: env-example
    type: file
    path: .env.example
  - id: main-go
    type: file
    path: main.go
---

Local development supports two primary workflows: running with Docker Compose for a complete environment, or running the Go backend and Vite frontend individually for iterative development.

## Docker Compose Workflow

Docker Compose starts all infrastructure services (PostgreSQL, Redis, MinIO, e2b-local) and the application server (oma-server) with a single command [@docker-compose]. This is the fastest way to get a complete environment running.

Start all services:

```bash
docker compose up -d
```

The console frontend becomes available at `http://localhost:28080` and the API at `http://localhost:38080` [@docker-compose].

Configure optional upstream Anthropic API credentials in `.env`:

```bash
ANTHROPIC_UPSTREAM_API_KEY=sk-ant-...
ANTHROPIC_UPSTREAM_BASE_URL=https://your-proxy.example.com
```

Stop services when done:

```bash
docker compose down
```

Include `-v` to delete data volumes:

```bash
docker compose down -v
```

## Manual Backend Development

For backend development, use the `just` command runner to restart the server [@justfile]. The server runs in the foreground and restarts automatically when you press Ctrl+C and run the command again.

Start the backend server on the default port (38080):

```bash
just restart-server
```

The script stops any process already listening on the port, waits for it to release, and then starts `go run .` with the address `127.0.0.1:38080` [@restart-server].

Override the port or address:

```bash
PORT=18080 ADDR=127.0.0.1:18080 just restart-server
```

## Manual Frontend Development

The frontend Vite development server runs on port 5173 by default and proxies `/api` and `/v1` requests to the backend [@restart-web].

Start the frontend:

```bash
just restart-web
```

The script intelligently handles port conflicts: it only stops listeners from the current checkout and automatically selects the next available port if another process owns the requested port [@restart-web].

Override the frontend port or API target:

```bash
PORT=4173 API_PORT=18080 just restart-web
```

## Environment Configuration

The application loads configuration from environment variables. For Docker Compose, copy `.env.example` to `.env` and edit values [@env-example]. For manual development, set environment variables before running `go run .` or rely on the compiled-in defaults in `config/` [@main-go].

Key configuration variables:
- `DATABASE_URL`: PostgreSQL connection string
- `REDIS_URL`: Redis connection string
- `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`: Object storage
- `E2B_API_URL`: Code interpreter sandbox URL

## Workflow Patterns

When developing full-stack changes, run both backend and frontend in parallel:

```bash
# Terminal 1
just restart-server

# Terminal 2
just restart-web
```

When only modifying backend code, run just the server. When only modifying frontend code, run just the Vite dev server with an existing backend.

The Docker Compose workflow is best for testing integration with infrastructure services, while the manual workflow provides faster iteration during feature development.
