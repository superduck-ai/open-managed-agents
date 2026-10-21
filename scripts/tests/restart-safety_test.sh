#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/repo/scripts" "$fixture/repo/oma-brain-dsh/scripts" "$fixture/repo/.run/generated/secrets" "$fixture/bin"
fixture="$(cd "$fixture" && pwd -P)"
export MOCK_ROOT="$fixture/repo"
git init -q "$MOCK_ROOT"
mkdir -p "$MOCK_ROOT/.worktrees/other"
git init -q "$MOCK_ROOT/.worktrees/other"
cp "$repo_root/scripts/restart-server.sh" "$MOCK_ROOT/scripts/"
cp "$repo_root/oma-brain-dsh/scripts/restart.sh" "$MOCK_ROOT/oma-brain-dsh/scripts/"
printf '{"name":"test"}\n' > "$MOCK_ROOT/.run/env.json"
printf '{"phase": "complete", "api_port": 40123}\n' > "$MOCK_ROOT/.run/generated/state.json"
touch "$MOCK_ROOT/.run/generated/config.yaml"
printf '#!/bin/sh\nexit 0\n' > "$MOCK_ROOT/scripts/generate-go.sh"
cat > "$fixture/bin/portless" <<'MOCK'
#!/bin/sh
if [ "$1" = get ]; then echo http://oma-test-api.localhost; fi
MOCK
cat > "$fixture/bin/bun" <<'MOCK'
#!/bin/sh
if [ "$OMA_API_URL" = http://oma-test-api.localhost ] && [ "$OMA_BRAIN_CLAIM_TOKEN" = fixture-token ]; then
  echo 'fixture credentials exported'
  exit 7
fi
exit 99
MOCK
cat > "$fixture/bin/lsof" <<'MOCK'
#!/bin/sh
case " $* " in
  *' -d cwd '*) echo "n$MOCK_ROOT/.worktrees/other" ;;
  *) echo 99999999 ;;
esac
MOCK
chmod +x "$fixture/bin/"* "$MOCK_ROOT/scripts/generate-go.sh"
export PATH="$fixture/bin:$PATH"
unset OMA_API_URL OMA_BRAIN_CLAIM_TOKEN
for scenario in missing empty whitespace valid; do
  token="$MOCK_ROOT/.run/generated/secrets/brain-claim-token"
  case "$scenario" in
    missing) rm -f "$token" ;;
    empty) : > "$token" ;;
    whitespace) printf ' \t\n' > "$token" ;;
    valid) printf fixture-token > "$token" ;;
  esac
  status=0
  bash "$MOCK_ROOT/oma-brain-dsh/scripts/restart.sh" > "$fixture/output" 2>&1 || status=$?
  if [[ "$scenario" == valid ]]; then
    [[ "$status" -eq 7 ]] && grep -q 'fixture credentials exported' "$fixture/output"
  else
    [[ "$status" -ne 0 ]] && ! grep -q 'fixture credentials exported' "$fixture/output"
    if [[ "$scenario" != missing ]]; then grep -q 'claim token is empty' "$fixture/output"; fi
  fi
  echo "brain token $scenario: passed"
done
status=0
OMA_START_LOCK_FD=3 bash "$MOCK_ROOT/scripts/restart-server.sh" --start-locked > "$fixture/output" 2>&1 || status=$?
[[ "$status" -ne 0 ]] && grep -q 'outside this worktree' "$fixture/output"
echo 'backend nested worktree listener: passed'
cat > "$fixture/bin/go" <<'MOCK'
#!/bin/bash
set -euo pipefail
if [[ "$1" == build ]]; then
  echo build >> "$MOCK_ROOT/events"
  cat > "$3" <<'SERVER'
#!/bin/sh
echo $$ > "$MOCK_ROOT/server.pid"
while ! test -f "$MOCK_ROOT/release"; do sleep 0.01; done
SERVER
  chmod +x "$3"
elif [[ "$3" == start-locked ]]; then
  shift 3
  export OMA_START_LOCK_FD=3
  exec "$@" 3>"$MOCK_ROOT/ready"
elif [[ "$3" == set-runtime ]]; then
  echo runtime >> "$MOCK_ROOT/events"
else
  exit 99
fi
MOCK
cat > "$fixture/bin/lsof" <<'MOCK'
#!/bin/sh
case " $* " in
  *' -p '*'-iTCP:'*) echo listening >> "$MOCK_ROOT/events"; cat "$MOCK_ROOT/server.pid" 2>/dev/null || true ;;
  *'40123'*) exit 0 ;;
  *) echo probe >> "$MOCK_ROOT/events" ;;
esac
MOCK
chmod +x "$fixture/bin/go" "$fixture/bin/lsof"
bash "$MOCK_ROOT/scripts/restart-server.sh" > "$fixture/output" 2>&1 &
restart_pid=$!
for _ in {1..100}; do
  [[ ! -s "$MOCK_ROOT/ready" ]] || break
  sleep 0.05
done
touch "$MOCK_ROOT/release"
status=0
wait "$restart_pid" || status=$?
[[ "$status" -eq 0 ]] && [[ "$(cat "$MOCK_ROOT/ready")" == . ]]
[[ "$(head -n 1 "$MOCK_ROOT/events")" == build ]]
grep -q listening "$MOCK_ROOT/events"
echo 'backend build and listener lock handoff: passed'
