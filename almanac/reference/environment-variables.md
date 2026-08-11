---
title: "Environment Variables"
summary: "Environment variables control all aspects of application behavior from service endpoints to resource limits."
topics: [configuration]
sources:
  - id: env-example
    type: file
    path: .env.example
  - id: config-go
    type: file
    path: internal/config/config.go
---

Environment variables are the primary mechanism for configuring the application. All variables are optional for local development due to built-in defaults, but production deployments require explicit values for critical settings. The config package automatically loads `.env` files from the project root before reading environment variables.

## Server and Database

`ADDR` (default `:8080`) sets the HTTP server listen address. `APP_ENV` (default `development`) controls environment-specific defaults like auto-migration behavior. `DATABASE_URL` (PostgreSQL connection string) and `POSTGRES_ADMIN_URL` are required for database connectivity. `DB_AUTO_MIGRATE` controls whether schema migrations run automatically; it defaults to true in development but false in production environments [@config-go][@env-example].

## Cache and Storage

`REDIS_URL` (default `redis://localhost:6379`) configures the Redis connection for caching. S3-compatible storage requires `S3_ENDPOINT` (default `http://localhost:9000`), `S3_BUCKET` (default `claude-files`), `S3_REGION` (default `us-east-1`), `S3_ACCESS_KEY_ID`, and `S3_SECRET_ACCESS_KEY`. `S3_FORCE_PATH_STYLE` (default `true`) controls whether the S3 client uses path-style addressing [@config-go].

## Anthropic API Integration

`ANTHROPIC_UPSTREAM_BASE_URL` (default `https://api.anthropic.com`) specifies the upstream Anthropic API endpoint. `ANTHROPIC_UPSTREAM_API_KEY` contains the API key for making requests to Anthropic. `PUBLIC_BASE_URL` sets the base URL for public-facing API responses. `ANTHROPIC_WEBHOOK_SIGNING_KEY` is used to validate webhook signatures from Anthropic [@config-go][@env-example].

## Batch Processing

Batch processing behavior is controlled by `BATCH_WORKER_ENABLED` (default `true`), `BATCH_WORKER_CONCURRENCY` (default `2`), and `BATCH_MAX_REQUESTS` (default `100000`). `BATCH_MAX_BODY_BYTES` (default `256MB`) limits batch request sizes. `BATCH_RESULT_RETENTION_DAYS` (default `29`) controls how long batch results are stored. Timeout settings include `BATCH_UPSTREAM_TIMEOUT` (default `10m`), `BATCH_JOB_LEASE_DURATION` (default `2m`), `BATCH_JOB_LEASE_HEARTBEAT_INTERVAL` (default `30s`), and `BATCH_EXPIRY_SWEEP_INTERVAL` (default `5m`) [@config-go].

## Environment Runner

The E2B sandbox environment runner uses `E2B_API_KEY`, `E2B_ACCESS_TOKEN`, `E2B_DOMAIN`, `E2B_API_URL`, `E2B_SANDBOX_URL`, and `E2B_TEMPLATE` (default `claude-code-interpreter`). Timeout settings are `E2B_REQUEST_TIMEOUT` (default `60s`) and `E2B_SANDBOX_TIMEOUT` (default `5m`). `E2B_DEBUG` enables debug logging. `ENVIRONMENT_RUNNER_ENABLED` (default `true`) and `ENVIRONMENT_RUNNER_CONCURRENCY` (default `2`) control the runner itself [@config-go].

## File and Resource Limits

`MAX_FILE_BYTES` (default `500MB`) sets the maximum size for uploaded files. `WORKSPACE_STORAGE_LIMIT_BYTES` (default `500GB`) limits total storage per workspace [@config-go].

## Webhooks

`WEBHOOK_ENDPOINT_URL` specifies where webhooks are delivered. `WEBHOOK_SIGNING_KEY` is required for webhook authentication. `WEBHOOK_EVENT_TYPES` (comma-separated list) defines which events trigger webhooks, defaulting to a comprehensive set of session and vault events. `WEBHOOK_TIMEOUT` (default `10s`) and `WEBHOOK_MAX_ATTEMPTS` (default `10`) control delivery behavior. `WEBHOOK_ALLOW_INSECURE` controls whether unsigned webhook events are accepted [@config-go].

## Code Session Configuration

Code session OTLP logging uses `CODE_SESSION_OTLP_FILE_LOG_ENABLED` (default `true` in development, `false` in production), `CODE_SESSION_OTLP_LOG_ROOT` (default `logs`), and `CODE_SESSION_OTLP_LOG_BODY_PREVIEW_BYTES` (default `256KB`). Session API URLs are configured via `CODE_SESSION_API_BASE_URL` and `CODE_SESSION_SANDBOX_API_BASE_URL`, both falling back to `PUBLIC_BASE_URL` when unset [@config-go].

## SDK Test Fixtures

Test fixtures for official SDK compatibility testing use `OFFICIAL_SDK_FIXTURE_*` variables. These provide predefined IDs for agents, environments, sessions, batches, and other resources used in automated tests. Most are hardcoded defaults, but agent ID, reference agent ID, and environment ID can be overridden via environment variables [@config-go].
