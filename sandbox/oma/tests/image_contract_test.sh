#!/usr/bin/env bash

set -euo pipefail

image=${1:?usage: image_contract_test.sh IMAGE}

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

run() {
  docker run --rm --platform linux/amd64 "$image" "$@"
}

[[ $(docker image inspect "$image" --format '{{.Os}}/{{.Architecture}}') == linux/amd64 ]] ||
  fail 'image is not Linux AMD64'

contract_tmp=$(mktemp -d)
invalid_log="${contract_tmp}/invalid.log"
trap 'rm -rf "$contract_tmp"' EXIT
invalid_selections=(
  ENV_PYTHON_VERSION=99
  ENV_NODE_VERSION=99
  ENV_RUBY_VERSION=99
  ENV_RUST_VERSION=99
  ENV_GO_VERSION=99
  ENV_BUN_VERSION=99
  ENV_PHP_VERSION=99
  ENV_JAVA_VERSION=99
  ENV_SWIFT_VERSION=99
)
for selection in "${invalid_selections[@]}"; do
  selector=${selection%%=*}
  version=${selection#*=}
  if docker run --rm --network none --platform linux/amd64 -e "$selection" "$image" true >"$invalid_log" 2>&1; then
    fail "${selector}: unavailable Runtime Version unexpectedly succeeded"
  fi
  grep -F "${selector} \"${version}\" is not available in this image" "$invalid_log" >/dev/null ||
    fail "${selector}: unavailable Runtime Version did not emit the safe error"
done

forbidden_prefix=CODE"X"
forbidden_name="${forbidden_prefix}_ENV_NODE_VERSION"
env_output=$(docker run --rm --platform linux/amd64 -e "$forbidden_name=20" "$image" env)
if grep -F "${forbidden_name}=" <<<"$env_output" >/dev/null; then
  fail 'entrypoint passed an unsupported provider selector'
fi
supplier_name=code"x"
if grep -Eiv 'org.opencontainers.image|upstream' <<<"$env_output" |
  grep -Ei "openai|${supplier_name}" >/dev/null; then
  fail 'entrypoint emitted an upstream welcome banner'
fi

default_checks=$(cat <<'CHECKS'
set -e
test "$(id -u)" = 0
test "$HOME" = /root
python3 --version | grep -E '^Python 3\.12([.]|$)'
node --version | grep -E '^v22([.]|$)'
ruby --version | grep -E '^ruby 3\.4\.4'
rustc --version | grep -E '^rustc 1\.89\.0 '
go version | grep -F 'go1.25.1 '
bun --version | grep -Fx '1.2.14'
php -r 'exit(PHP_MAJOR_VERSION === 8 && PHP_MINOR_VERSION === 4 ? 0 : 1);'
java -version 2>&1 | grep -E 'version "21([.]|\")'
swift --version | grep -E '^Swift version 6\.1([.]| |$)'
id user
id claude
sudo -n -u user true
sudo -n -u claude true
test -x /opt/env-runner/environment-manager
test -x /opt/claude-code/bin/claude
environment-manager --help >/dev/null
/opt/claude-code/bin/claude --version >/dev/null
grep -F 'mirrors.tuna.tsinghua.edu.cn/ubuntu' /etc/apt/sources.list.d/ubuntu.sources
grep -F 'https://pypi.tuna.tsinghua.edu.cn/simple' /etc/pip.conf
! grep -F 'trusted-host' /etc/pip.conf
test -f /usr/share/doc/oma-sandbox/upstream/LICENSE
test -f /usr/share/doc/oma-sandbox/upstream/codex-universal-image-sbom.spdx.json
test -z "${NODE_PATH:-}"
[[ $PATH != *'/home/claude/'* ]]
test ! -e /home/claude/.npm-global
test ! -e /home/claude/.config/pip/pip.conf
CHECKS
)
run bash -lc "$default_checks" >/dev/null || fail 'default image contract failed'
run node --version | grep -E '^v22([.]|$)' >/dev/null ||
  fail 'ordinary container commands do not inherit the selected Runtime'

docker run --rm --platform linux/amd64 \
  -e ENV_PYTHON_VERSION=3.11 \
  -e ENV_NODE_VERSION=20 \
  -e ENV_RUBY_VERSION=3.3.8 \
  -e ENV_RUST_VERSION=1.88.0 \
  -e ENV_GO_VERSION=1.24.3 \
  -e ENV_BUN_VERSION=1.2.14 \
  -e ENV_PHP_VERSION=8.3 \
  -e ENV_JAVA_VERSION=17 \
  -e ENV_SWIFT_VERSION=5.10 \
  "$image" \
  bash -lc '
    set -e
    python3 --version | grep -E "^Python 3[.]11([.]|$)"
    node --version | grep -E "^v20([.]|$)"
    ruby --version | grep -E "^ruby 3[.]3[.]8"
    rustc --version | grep -E "^rustc 1[.]88[.]0 "
    go version | grep -F "go1.24.3 "
    bun --version | grep -Fx "1.2.14"
    php -r "exit(PHP_MAJOR_VERSION === 8 && PHP_MINOR_VERSION === 3 ? 0 : 1);"
    java -version 2>&1 | grep -E "version .17([.]|.)"
    swift --version | grep -E "^Swift version 5[.]10([.]| |$)"
  ' >/dev/null || fail 'explicit installed-version contract failed'

run bash -lc '/opt/oma/setup_runtime.sh >/tmp/first && /opt/oma/setup_runtime.sh >/tmp/second && cmp /tmp/first /tmp/second' >/dev/null ||
  fail 'Runtime Setup is not idempotent'

standalone_setup_checks=$(cat <<'CHECKS'
set -e
mkdir -p /tmp/oma-conflicting-workspace
cd /tmp/oma-conflicting-workspace
printf '3.10\n' >.python-version
printf '99\n' >.php-version
printf '[toolchain]\nchannel = "99"\n' >rust-toolchain.toml
printf '[tools]\njava = "17"\n' >mise.toml
/opt/oma/setup_runtime.sh >/tmp/standalone-setup.log
/bin/bash --login -c '
  set -e
  cd /
  cd /tmp/oma-conflicting-workspace
  python3 --version | grep -E "^Python 3[.]12([.]|$)"
  node --version | grep -E "^v22([.]|$)"
  ruby --version | grep -E "^ruby 3[.]4[.]4"
  rustc --version | grep -E "^rustc 1[.]89[.]0 "
  go version | grep -F "go1.25.1 "
  bun --version | grep -Fx "1.2.14"
  php -r "exit(PHP_MAJOR_VERSION === 8 && PHP_MINOR_VERSION === 4 ? 0 : 1);"
  java --version 2>&1 | grep -E "version .21([.]|.)"
  [[ $JAVA_HOME == /root/.local/share/mise/installs/java/21.* ]]
  "$JAVA_HOME/bin/java" --version 2>&1 | grep -E "version .21([.]|.)"
  swift --version | grep -E "^Swift version 6[.]1([.]| |$)"
'
CHECKS
)
docker run --rm --network none --platform linux/amd64 --entrypoint /bin/bash \
  "$image" -lc "$standalone_setup_checks" >/dev/null ||
  fail 'standalone Setup did not persist selected Runtimes across a conflicting workspace'

printf 'printf "OMA login shell exit\\n"\n' >"${contract_tmp}/bash_logout"
no_command_output=$(docker run --rm --platform linux/amd64 \
  --mount "type=bind,src=${contract_tmp}/bash_logout,dst=/root/.bash_logout,readonly" \
  "$image" </dev/null)
grep -F 'OMA login shell exit' <<<"$no_command_output" >/dev/null ||
  fail 'empty entrypoint did not expose a true login shell'

printf 'sandbox image contract: PASS\n'
