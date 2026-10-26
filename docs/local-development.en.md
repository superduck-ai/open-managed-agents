# Local development and operations

[Back to the README](../README.md)

This guide covers configuration, private keys, source-based startup, database migrations, and testing. Run commands from the repository root unless a step explicitly changes directories. For the shortest deployment path, use the [Compose quickstart](../README.md#quickstart).

For automatic worktree setup, shared model providers, portless aliases, Brain startup, and environment recycling, see the [worktree development guide](./development-guide.md).

## Local dependencies

For isolated worktree environments and Brain conversations, run `just dev-init` to prepare dependencies, local configuration, keys, and development data. Configure LLM providers and credentials once under `~/.oma-dev/`; worktrees reference the shared file. Then run `just restart` in Herdr to manage the backend, Brain, and Web. See the [local development guide](./development-guide.md) for initial LLM configuration and daily operations.

You will need:

- Go matching the version declared in `go.mod`.
- Bun for frontend development, testing, and builds.
- PostgreSQL. The default connection string is `postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable`.
- Redis, defaulting to `redis://localhost:6379`.
- A three-node NATS JetStream cluster, using local ports `4222`, `4223`, and `4224` by default. `nats.url` is required at startup and accepts comma-separated seed nodes.
- MinIO or another S3-compatible store. Local defaults are endpoint `http://localhost:9000`, bucket `claude-files`, and credentials `minioadmin/minioadmin`.

Before starting the service, make sure the PostgreSQL database and role in `database.url` exist. If the initial connection fails, the application only attempts local initialization using connections to `postgres` or `template1` derived from that URL and the current system user. A separate administrator connection string is no longer supported.

## Configuration

Copy the example configuration and edit it as needed:

```bash
mkdir -p .run
cp config/config.example.yaml .run/config.yaml
```

The application searches upward from the current directory for `.run/config.yaml`, stopping at `go.mod`. You can select a file explicitly with `CONFIG_FILE=/path/to/config.yaml`. A configuration file is required. YAML fields are validated strictly, so unknown field names prevent startup. The top-level `env` accepts only `dev` or `prod`.

Common local settings:

```yaml
env: dev

server:
  addr: 127.0.0.1:38080

database:
  url: postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable

redis:
  url: redis://localhost:6379

nats:
  url: nats://localhost:4222,nats://localhost:4223,nats://localhost:4224

storage:
  type: s3
  s3:
    endpoint: http://localhost:9000
    bucket: claude-files
    region: us-east-1
    access_key_id: minioadmin
    secret_access_key: minioadmin
    force_path_style: true

e2b:
  api_key: ""
  api_url: ""
```

YAML settings override code defaults. Business configuration environment variables do not override YAML. `CONFIG_FILE` only selects the configuration file, and variables such as `${HOME}` in path fields only expand paths. In production, mount the complete YAML as a read-only Secret and restrict file permissions.

Upgrading from a version that uses `.env` or business configuration environment variables is a breaking change. Generate and validate a complete YAML configuration before upgrading. The current version does not fall back to the old variables. See [runtime configuration migration](./design/be/runtime-configuration.md#从-env-迁移) for field mappings, removed settings, and rollback steps.

### Create Code Session private keys

Create a directory accessible only to your user, then generate both private keys with the repository scripts:

```bash
mkdir -p .run/secrets
chmod 700 .run/secrets

just generate-code-session-jwt-key \
  .run/secrets/code-session-jwt-ed25519.pem

just generate-upstream-proxy-ca-key \
  .run/secrets/upstream-proxy-ca-key.pem
```

Both scripts set file permissions to `0600` and refuse to overwrite an existing file to prevent accidental key rotation. Git ignores `.run/config.yaml` and private keys under `.run/secrets/`. YAML paths support `${HOME}` and `~/`. Relative paths resolve against the directory containing the configuration file.

```yaml
code_session:
  jwt_signing_private_key_file: secrets/code-session-jwt-ed25519.pem
  upstream_proxy_mitm_enabled: true
  upstream_proxy_ca_key_file: secrets/upstream-proxy-ca-key.pem
```

`code_session.jwt_signing_private_key_file` points to an Ed25519 private key in PKCS#8 PEM format for signing session-ingress JWTs. Development and test environments generate an ephemeral in-process key when this setting is omitted. Production refuses to start without it.

`code_session.upstream_proxy_ca_key_file` points to an ECDSA P-256 private key in PKCS#8 PEM format. CCRv2 HTTPS MITM is enabled by default, so the CA key is required even if the switch is omitted. Set `code_session.upstream_proxy_mitm_enabled: false` to disable it. At startup, the service uses the key to issue a root certificate in memory without writing a certificate file to disk.

`config/config.example.yaml` is the minimal configuration for local development. Generate its referenced CA private key before use, or run `just dev-init` to prepare configuration and keys automatically. Settings that usually need no adjustment, such as Batch, Webhook, and Environment Runner options, retain their code defaults and are omitted from the example. See [the configuration reference](./configuration-reference.yaml) for all supported fields and safe example values.

Configuration loading starts in `internal/config/config.go`, and domain types live in `internal/config/types.go`. Runtime settings are grouped by responsibility, including database, storage, Batch, Code Session, and Webhook. If `database.auto_migrate` is omitted, it defaults to enabled in development and disabled in production.

## Start the backend

Run the restart task from the repository root. For an unbound checkout, it releases the configured default port and runs the service in the foreground:

```bash
just restart-server
```

For Managed Agent Sandbox debugging, Filestore FUSE mounts, or Environment Runner work, use Docker Compose for PostgreSQL, Redis, NATS, and MinIO, then start e2b-local, oma-server, and Vite separately from source on the host. See [local development mode](./design/docker-compose-deployment.md#8-本地开发模式) for startup order and configuration.

For an unbound checkout, the restart script requires `.run/config.yaml` and checks it before releasing the port. Create it from `config/config.example.yaml` before the first run.

The `server.addr` setting determines the listening address. If you change its port, pass the matching `PORT` to the restart task:

```yaml
server:
  addr: 127.0.0.1:18080
```

```bash
PORT=18080 just restart-server
```

For a worktree bound by `just dev-init`, restart tasks use `.run/generated/config.yaml`, check service ownership, and allocate ports dynamically. Use `just slot-info` to inspect the environment and its addresses. Browser access uses the portless aliases `http://oma-<name>-api.localhost` and `http://oma-<name>-web.localhost`. See [worktree development environments](./design/development-worktree-environments.md).

Check the health endpoint using your backend address. For the unbound `18080` example above:

```bash
curl http://127.0.0.1:18080/healthz
```

Expected response:

```json
{ "status": "ok" }
```

## Start the frontend

Install frontend dependencies:

```bash
cd web
bun install
```

Then start Vite from the repository root:

```bash
just restart-web
```

For an unbound checkout, the default frontend address is `http://127.0.0.1:5173`. It proxies `/api` and `/v1` to `http://127.0.0.1:38080`. If the backend uses another port:

```bash
API_PORT=18080 PORT=5173 just restart-web
```

If another checkout or process owns the requested frontend port, the restart script preserves that process and selects the next available port. Bound worktrees use their generated configuration and portless aliases.

Local console login uses a magic-link-compatible interface. The backend creates and resolves platform sessions for local console development.

## Database and migrations

Goose migrations in `internal/db/migrations` manage the database schema. To run migrations manually, generate the required Go sources first:

```bash
./scripts/generate-go.sh
go run ./cmd/migrate up
```

The project does not use PostgreSQL foreign key constraints. After applying goose migrations, `DB.Migrate()` removes any foreign key constraints found in the current schema. A guard test in `tests/files_api_test.go` checks this behavior.

For schema changes:

- Add a new numbered SQL file under `internal/db/migrations`, such as `00010_add_xxx.sql`.
- Do not modify migrations that have already been applied.
- Do not append new schema changes to `internal/db/schema.go`.
- Include explicit organization or workspace scope in multi-tenant queries and writes.

## Tests

Run ordinary backend unit tests with:

```bash
just test
```

This generates the required Go sources, then runs `go test ./... -count=1` without integration tags. Ordinary unit tests must not connect to configured external services.

Tests that require real PostgreSQL, Redis, S3, or external SDKs and CLIs use the `integration` build tag. Run the smallest relevant set with `just test-integration` and isolated test configuration and dependencies.

To test object storage compatibility against local MinIO or another S3-compatible service:

```bash
OMA_S3_INTEGRATION_ENDPOINT=http://127.0.0.1:9000 \
  just test-integration ./internal/storage -run '^TestS3CompatibleIntegration$' -v
```

Defaults are bucket `claude-files`, region `us-east-1`, and local MinIO credentials `minioadmin/minioadmin`. Override them with `OMA_S3_INTEGRATION_BUCKET`, `OMA_S3_INTEGRATION_REGION`, `OMA_S3_INTEGRATION_ACCESS_KEY_ID`, and `OMA_S3_INTEGRATION_SECRET_ACCESS_KEY`.

The test covers repeated bucket creation, small objects, multipart uploads with known and unknown lengths, downloads, and deletion. It removes test objects but preserves the bucket by default. To test bucket creation and automatic cleanup, use a unique bucket name starting with `oma-storage-test-` and set `OMA_S3_INTEGRATION_DELETE_BUCKET=1`.

Run frontend tests and builds with:

```bash
cd web
bun test
bun run build
```

Equivalent tasks from the repository root:

```bash
just web-test
just web-build
```

For real E2E tests, set `server.addr` to `127.0.0.1:18080` in an isolated test configuration and start the service:

```bash
./scripts/generate-go.sh
CONFIG_FILE=/path/to/test-config.yaml go run .
```

In another terminal, run the SDK test with the default development API key `sk-ant-local-default` available on the test server:

```bash
TEST_API_BASE_URL=http://127.0.0.1:18080 \
  just test-integration ./tests -run '^TestGoSDKFilesE2E$' -v
```

Stop all temporary local services after the E2E run.

## Development conventions

- Use `github.com/go-chi/chi/v5` for HTTP routing. Prefer `chi.Mount`, sub-routers, or route groups for new resources.
- Write `/v1/*` errors through `internal/httpapi.WriteError` to preserve the Anthropic-compatible response structure.
- Keep `internal/api` focused on assembly, middleware, authentication entry points, and resource mounting. Business rules, SQL, and resource-specific handling belong elsewhere.
- The DB layer returns ordinary Go errors or identifiable domain errors. It does not construct HTTP status codes or API JSON.
- Store object content, including files, skills, memory content, and batch results, in S3-compatible storage. Store metadata in PostgreSQL.
- Keep the frontend's `/api` console boundary distinct from the `/v1` SDK-compatible API. Console requirements must not change `/v1` behavior.
- After changing `web/`, run `just restart-web` from the repository root before browser verification.
