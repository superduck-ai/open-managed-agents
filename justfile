set shell := ["bash", "-euo", "pipefail", "-c"]

default:
  @just --list

help:
  @just --list

# Restart backend in foreground. Override with: PORT=18080 ADDR=127.0.0.1:18080 just server
server:
  PORT="${PORT:-38080}" ADDR="${ADDR:-127.0.0.1:${PORT:-38080}}" ./scripts/restart-server.sh

# Restart backend in foreground. Override with: PORT=18080 ADDR=127.0.0.1:18080 just restart-server
restart-server:
  PORT="${PORT:-38080}" ADDR="${ADDR:-127.0.0.1:${PORT:-38080}}" ./scripts/restart-server.sh

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

web-build:
  cd web && bun run build

web-test:
  cd web && bun test

web-lint:
  cd web && bun run lint

web-format:
  cd web && bun run format

web-format-check:
  cd web && bun run format:check
