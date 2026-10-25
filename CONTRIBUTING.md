# Contributing

Contributions can include bug fixes, SDK compatibility improvements, documentation, and runtime integrations. For a bug report, include reproduction steps, expected and actual behavior, and relevant versions. Remove credentials and private payloads from examples and logs.

## Prepare a development environment

Use the Go version in [go.mod](./go.mod), Bun, and the tools listed in the [development guide](./docs/development-guide.md). Run commands from the repository root unless stated otherwise.

```bash
just dev-init
```

On first use, configure `~/.oma-dev/model-providers.yaml` with your provider credentials and rerun initialization. The guide covers infrastructure, portless, Herdr, and service startup. For manual configuration and targeted tests, see [local development and operations](./docs/local-development.en.md).

Read [AGENTS.md](./AGENTS.md) before changing code and [web/AGENTS.md](./web/AGENTS.md) for frontend work. Install the managed hook if it is missing:

```bash
just hooks-install
```

## Validate a change

Run ordinary unit tests by default:

```bash
just test
```

This generates the ignored Go sources before testing. For direct Go build or analysis commands, first run `./scripts/generate-go.sh`. Generated `*.gen.go` files must not be committed.

For Go changes, run:

```bash
just lint
just dead-code
```

For Go or TypeScript production changes, also run:

```bash
just duplicates
just complexity
```

For frontend changes, run the relevant feature tests and:

```bash
just web-format-check
just web-build
```

Before submitting, run the repository-wide checks:

```bash
just hooks-run
just large-files
```

Do not bypass failing hooks or raise quality budgets to hide failures. Tracked files must be no larger than 1 MiB.

Tests requiring real PostgreSQL, Redis, S3, or external SDKs and CLIs belong behind the `integration` build tag. Use isolated configuration and dependencies, and select the smallest relevant integration test scope explicitly. For example:

```bash
just test-integration ./tests -run '^TestGoSDKFilesE2E$' -v
```

Report what you ran and any integration coverage you did not verify. Agent-driven `verify-be` runs require an explicit user request; ordinary unit tests and static checks remain required as applicable.

## Keep implementation boundaries clear

- Keep `internal/api` focused on assembly, middleware, and routing. Resource packages own validation and business operations.
- Keep HTTP DTOs, domain types, and database rows separate where their semantics differ. The DB layer must not depend on handlers or construct HTTP responses.
- Use Yourbatis Mappers for runtime SQL. Read the [Yourbatis guidelines](./docs/design/be/yourbatis-guidelines.md) before modifying a Mapper.
- Preserve Anthropic-compatible API behavior and explicit organization/workspace scope.
- Use `log/slog` for structured runtime logs and keep credentials and raw payloads out of logs.
- Follow the repository rule against code comments.

## Database changes

Add a new numbered goose migration under `internal/db/migrations`. Do not edit applied migrations or append schema changes to `internal/db/schema.go`.

The project does not use PostgreSQL foreign keys. `DB.Migrate()` removes foreign key constraints after migrations; retain the no-FK guard in `tests/files_api_test.go`. Cross-table references use resource UUIDs, and application writes and tests enforce integrity.

Read the [runtime configuration design](./docs/design/be/runtime-configuration.md) when changing settings. Keep real keys in ignored local files or an external Secret manager.

## Submit a focused change

Explain the problem, the resulting behavior, and validation. Update the relevant `docs/design/` document when behavior, public contracts, state transitions, data models, permissions, architecture, or acceptance paths change. If the existing design already describes the implementation, explain why no design update is needed.

Create Draft PRs through the locally authenticated `gh` CLI, following the identity checks in [AGENTS.md](./AGENTS.md). Keep unrelated formatting and refactoring out of the change.
