#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"

BINDING=.run/env.json
CONFIG_FILE="$REPO_ROOT/.run/config.yaml"
if [[ -f "$BINDING" ]]; then
  CONFIG_FILE="$REPO_ROOT/.run/generated/config.yaml"
fi
export CONFIG_FILE

WORKER_RECIPE="$(bun -e '
  try {
    const cfg = Bun.YAML.parse(await Bun.file(process.env.CONFIG_FILE).text());
    const enabled = cfg?.environment_runner?.cloud_harness;
    if (enabled !== undefined && typeof enabled !== "boolean") throw new Error();
    console.log(enabled ? "restart-harness" : "restart-brain");
  } catch {
    console.error("Cannot read environment_runner.cloud_harness from " + process.env.CONFIG_FILE);
    process.exit(1);
  }
')"
if [[ "${1:-}" == --recipe ]]; then
  printf '%s\n' "$WORKER_RECIPE"
  exit
fi
if [[ "$WORKER_RECIPE" != restart-harness ]]; then
  echo "Enable environment_runner.cloud_harness and restart the backend before starting the Harness Worker." >&2
  exit 1
fi

if [[ -f "$BINDING" ]]; then
  if ! grep -q '"phase": "complete"' .run/generated/state.json 2>/dev/null; then
    echo "Environment initialization is not complete; run just dev-init before starting the Harness Worker." >&2
    exit 1
  fi
  API_ROUTE="$(bun -e '
    const binding = await Bun.file(".run/env.json").json();
    if (typeof binding.name !== "string" || !binding.name) process.exit(1);
    console.log("oma-" + binding.name + "-api");
  ')"
  OMA_API_URL="$(portless get "$API_ROUTE" --no-worktree)"
else
  OMA_API_URL="${OMA_API_URL:-$(bun -e '
    const cfg = Bun.YAML.parse(await Bun.file(process.env.CONFIG_FILE).text());
    const address = cfg.server?.addr ?? "127.0.0.1:38080";
    console.log("http://" + (address.startsWith(":") ? "127.0.0.1" : "") + address);
  ')}"
fi

WORKER_BINARY="$REPO_ROOT/tmp/dev-harness/exe/oma-harness-worker"
./scripts/generate-go.sh --cached
mkdir -p "$(dirname "$WORKER_BINARY")"
go build -o "$WORKER_BINARY" ./cmd/oma-harness-worker

WORKER_PID=
stop_worker() {
  if [[ -n "$WORKER_PID" ]]; then
    kill -TERM "$WORKER_PID" 2>/dev/null || true
    wait "$WORKER_PID" 2>/dev/null || true
  fi
  exit
}
trap stop_worker INT TERM

while :; do
  started_at=$SECONDS
  sh -c '
    printf "harness worker process started pid=%s\n" "$$"
    exec "$@"
  ' oma-harness-worker "$WORKER_BINARY" -api-url "$OMA_API_URL" -serve &
  WORKER_PID=$!
  status=0
  wait "$WORKER_PID" || status=$?
  printf 'harness worker process exited pid=%s status=%s elapsed=%ss\n' "$WORKER_PID" "$status" "$((SECONDS - started_at))"
  WORKER_PID=
  [[ "$status" -eq 0 ]] || sleep 1
done
