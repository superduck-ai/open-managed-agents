set shell := ["bash", "-euo", "pipefail", "-c"]

default:
  @just --list

help:
  @just --list

# Create the gitignored Docker Compose runtime config without overwriting an existing secret-bearing file.
init-compose-config:
  @compose_template="deploy/docker-compose/oma-server.yaml"; compose_local="deploy/docker-compose/oma-server.local.yaml"; \
    if [[ -e "$compose_local" || -L "$compose_local" ]]; then \
      echo "$compose_local already exists; leaving it unchanged"; \
    else \
      install -m 600 "$compose_template" "$compose_local"; \
      echo "created $compose_local with mode 0600"; \
    fi

# Restart backend in foreground. server.addr comes from config/config.yaml; PORT selects the listener to stop.
server:
  PORT="${PORT:-38080}" ./scripts/restart-server.sh

# Restart backend in foreground. server.addr comes from config/config.yaml; PORT selects the listener to stop.
restart-server:
  PORT="${PORT:-38080}" ./scripts/restart-server.sh

# Restart frontend Vite dev server in foreground. Override with: PORT=4173 API_PORT=18080 just web
# Only stops listeners from this checkout; uses the next port when another checkout/process owns the requested port.
web:
  PORT="${PORT:-5173}" HOST="${HOST:-127.0.0.1}" API_PORT="${API_PORT:-38080}" VITE_API_PROXY_TARGET="${VITE_API_PROXY_TARGET:-http://127.0.0.1:${API_PORT:-38080}}" ./scripts/restart-web.sh

# Restart frontend Vite dev server in foreground. Override with: PORT=4173 API_PORT=18080 just restart-web
# Only stops listeners from this checkout; uses the next port when another checkout/process owns the requested port.
restart-web:
  PORT="${PORT:-5173}" HOST="${HOST:-127.0.0.1}" API_PORT="${API_PORT:-38080}" VITE_API_PROXY_TARGET="${VITE_API_PROXY_TARGET:-http://127.0.0.1:${API_PORT:-38080}}" ./scripts/restart-web.sh

# Restart weather MCP server in foreground. Override with: PORT=39091 WEATHER_MCP_PATH=/custom just weather-mcp

test:
  go test ./... -count=1

# Run the repository's configured Go static-analysis and formatting checks.
lint:
  golangci-lint run --config .golangci.yml ./...

dead-code:
  ./scripts/go-dead-code.sh

duplicates:
  ./scripts/check-duplicates.sh

complexity: go-complexity web-complexity

# Generate the stable code session JWT signing key. Example: just generate-code-session-jwt-key config/secrets/code-session-jwt-ed25519.pem
generate-code-session-jwt-key output:
  ./scripts/generate-code-session-jwt-key.sh "{{ output }}"

test-generate-code-session-jwt-key:
  ./scripts/tests/generate-code-session-jwt-key_test.sh

# Generate the stable CCRv2 MITM CA key. Example: just generate-upstream-proxy-ca-key config/secrets/upstream-proxy-ca-key.pem
generate-upstream-proxy-ca-key output:
  ./scripts/generate-upstream-proxy-ca-key.sh "{{ output }}"

test-generate-upstream-proxy-ca-key:
  ./scripts/tests/generate-upstream-proxy-ca-key_test.sh

go-complexity:
  ./scripts/go-complexity.sh

web-build:
  cd web && bun run build

web-test:
  cd web && bun test

web-lint:
  cd web && bun run lint

web-complexity:
  cd web && bun run lint:complexity

web-lint-naming:
  cd web && bun run lint:naming

web-format:
  cd web && bun run format

web-format-check:
  cd web && bun run format:check

# Check every tracked file with the repository-pinned pre-commit hook.
large-files:
  ./scripts/pre-commit.sh run check-added-large-files --all-files

# Install the repository-managed pre-commit hook in the current Git clone.
hooks-install:
  ./scripts/pre-commit.sh install --install-hooks

# Run every configured pre-commit check against all tracked files.
hooks-run:
  ./scripts/pre-commit.sh run --all-files
