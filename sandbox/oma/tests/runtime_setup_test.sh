#!/usr/bin/env bash

set -euo pipefail

oma_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
test_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

fake_bin="${tmp_dir}/bin"
state_dir="${tmp_dir}/state"
mkdir -p "$fake_bin" "$state_dir"
for tool in pyenv nvm mise rustup phpenv swiftly python python3 node ruby rustc go bun php java swift curl wget; do
  ln -s "${test_dir}/fake-runtime-tool" "${fake_bin}/${tool}"
done

export OMA_TEST_STATE_DIR=$state_dir
export PATH="${fake_bin}:/usr/bin:/bin"
export PYENV_ROOT="${tmp_dir}/pyenv"
export NVM_DIR="${tmp_dir}/nvm"
export PHPENV_ROOT="${tmp_dir}/phpenv"
export SWIFTLY_BIN_DIR="${tmp_dir}/swiftly"
export CARGO_HOME="${tmp_dir}/cargo"
export RUSTUP_HOME="${tmp_dir}/rustup"
export MISE_DATA_DIR="${tmp_dir}/mise"
export OMA_TEST_REQUIRE_LOCAL_GO_TOOLCHAIN=1
export OMA_TEST_REQUIRE_MISE_OFFLINE=1
export OMA_TEST_REQUIRE_MISE_NO_CONFIG=1
export OMA_TEST_CONFLICTING_RUNTIME_CONFIG=1
runtime_profile="${tmp_dir}/profile.d/oma-runtime.sh"
setup_runtime_command=(
  /bin/bash -c 'source "$1"; oma_setup_runtime "$2"' oma-runtime-test
  "$oma_dir/setup_runtime.sh" "$runtime_profile"
)

link_runtime() {
  local bin_dir=$1
  local tool=$2
  mkdir -p "$bin_dir"
  ln -s "${test_dir}/fake-runtime-tool" "${bin_dir}/${tool}"
}

link_runtime "${PYENV_ROOT}/versions/3.12/bin" python
link_runtime "${PYENV_ROOT}/versions/3.11/bin" python
link_runtime "${NVM_DIR}/versions/node/v22.0.0/bin" node
link_runtime "${NVM_DIR}/versions/node/v20.0.0/bin" node
link_runtime "${MISE_DATA_DIR}/installs/ruby/3.4.4/bin" ruby
link_runtime "${MISE_DATA_DIR}/installs/ruby/3.3.8/bin" ruby
link_runtime "${RUSTUP_HOME}/toolchains/1.89.0-x86_64-unknown-linux-gnu/bin" rustc
link_runtime "${RUSTUP_HOME}/toolchains/1.88.0-x86_64-unknown-linux-gnu/bin" rustc
link_runtime "${MISE_DATA_DIR}/installs/go/1.25.1/bin" go
link_runtime "${MISE_DATA_DIR}/installs/go/1.24.3/bin" go
link_runtime "${MISE_DATA_DIR}/installs/bun/1.2.14/bin" bun
link_runtime "${PHPENV_ROOT}/versions/8.4snapshot/bin" php
link_runtime "${PHPENV_ROOT}/versions/8.3snapshot/bin" php
link_runtime "${MISE_DATA_DIR}/installs/java/21.0.8/bin" java
link_runtime "${MISE_DATA_DIR}/installs/java/17.0.15/bin" java
link_runtime "$SWIFTLY_BIN_DIR" swift

workspace_dir="${tmp_dir}/workspace"
mkdir -p "$workspace_dir"
printf '3.10\n' >"${workspace_dir}/.python-version"
printf '99\n' >"${workspace_dir}/.php-version"
printf '[toolchain]\nchannel = "99"\n' >"${workspace_dir}/rust-toolchain.toml"
printf '[tools]\njava = "99"\n' >"${workspace_dir}/mise.toml"
cd "$workspace_dir"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_state() {
  local runtime=$1
  local expected=$2
  local actual
  actual=$(cat "${state_dir}/${runtime}")
  [[ $actual == "$expected" ]] || fail "${runtime}: expected ${expected}, got ${actual}"
}

run_setup() {
  env -u ENV_PYTHON_VERSION \
      -u ENV_NODE_VERSION \
      -u ENV_RUBY_VERSION \
      -u ENV_RUST_VERSION \
      -u ENV_GO_VERSION \
      -u ENV_BUN_VERSION \
      -u ENV_PHP_VERSION \
      -u ENV_JAVA_VERSION \
      -u ENV_SWIFT_VERSION \
      OMA_TEST_STATE_DIR="$OMA_TEST_STATE_DIR" \
      PATH="$PATH" \
      "${setup_runtime_command[@]}"
}

test_unavailable_version_fails_without_network() {
  local selection selector version error_log
  local invalid_selections=(
    ENV_PYTHON_VERSION=99
    ENV_NODE_VERSION=99
    ENV_RUBY_VERSION=99
    ENV_RUST_VERSION=99
    ENV_GO_VERSION=99
    ENV_BUN_VERSION=99
    ENV_PHP_VERSION=99
    ENV_JAVA_VERSION=99
    ENV_JAVA_VERSION=2
    ENV_SWIFT_VERSION=99
  )

  for selection in "${invalid_selections[@]}"; do
    selector=${selection%%=*}
    version=${selection#*=}
    error_log="${tmp_dir}/invalid-${selector}.err"
    if env "$selection" "${setup_runtime_command[@]}" >"${tmp_dir}/invalid.log" 2>"$error_log"; then
      fail "${selector}: unavailable version unexpectedly succeeded"
    fi
    grep -F "${selector} \"${version}\" is not available in this image" "$error_log" >/dev/null ||
      fail "${selector}: unavailable version did not return the safe public error"
    ! grep -F 'network access attempted' "$error_log" >/dev/null ||
      fail "${selector}: unavailable version attempted a network download"
  done
}

test_provider_selector_is_scrubbed_for_direct_setup() {
  local forbidden_prefix=CODE"X"
  local forbidden_name="${forbidden_prefix}_ENV_NODE_VERSION"
  local provider_state="${tmp_dir}/provider-state"
  mkdir -p "$provider_state"

  env \
    OMA_TEST_STATE_DIR="$provider_state" \
    OMA_TEST_FORBIDDEN_ENV_NAME="$forbidden_name" \
    "$forbidden_name=20" \
    "${setup_runtime_command[@]}" >"${tmp_dir}/provider-selector.log"

  [[ $(cat "${provider_state}/node") == 22 ]] ||
    fail 'provider-prefixed selector changed the directly configured Node.js Runtime'
}

test_defaults_and_idempotency() {
  run_setup >"${tmp_dir}/first.log"
  assert_state python 3.12
  assert_state node 22
  assert_state ruby 3.4.4
  assert_state rust 1.89.0
  assert_state go 1.25.1
  assert_state bun 1.2.14
  assert_state php 8.4
  assert_state java 21
  assert_state swift 6.1

  run_setup >"${tmp_dir}/second.log"
  cmp "${tmp_dir}/first.log" "${tmp_dir}/second.log" || fail 'repeat setup changed its observable output'
}

test_empty_selectors_use_defaults() {
  ENV_PYTHON_VERSION='' \
  ENV_NODE_VERSION='' \
  ENV_RUBY_VERSION='' \
  ENV_RUST_VERSION='' \
  ENV_GO_VERSION='' \
  ENV_BUN_VERSION='' \
  ENV_PHP_VERSION='' \
  ENV_JAVA_VERSION='' \
  ENV_SWIFT_VERSION='' \
    "${setup_runtime_command[@]}" >"${tmp_dir}/empty.log"

  assert_state python 3.12
  assert_state node 22
  assert_state ruby 3.4.4
  assert_state rust 1.89.0
  assert_state go 1.25.1
  assert_state bun 1.2.14
  assert_state php 8.4
  assert_state java 21
  assert_state swift 6.1
}

test_explicit_installed_versions() {
  ENV_PYTHON_VERSION=3.11 \
  ENV_NODE_VERSION=20 \
  ENV_RUBY_VERSION=3.3.8 \
  ENV_RUST_VERSION=1.88.0 \
  ENV_GO_VERSION=1.24.3 \
  ENV_BUN_VERSION=1.2.14 \
  ENV_PHP_VERSION=8.3 \
  ENV_JAVA_VERSION=17 \
  ENV_SWIFT_VERSION=5.10 \
    "${setup_runtime_command[@]}" >"${tmp_dir}/override.log"

  assert_state python 3.11
  assert_state node 20
  assert_state ruby 3.3.8
  assert_state rust 1.88.0
  assert_state go 1.24.3
  assert_state bun 1.2.14
  assert_state php 8.3
  assert_state java 17
  assert_state swift 5.10
}

test_profile_persists_explicit_runtimes() {
  local profile_path java_home
  local expected_path="${SWIFTLY_BIN_DIR}:${MISE_DATA_DIR}/installs/java/17.0.15/bin:${PHPENV_ROOT}/versions/8.3snapshot/bin:${MISE_DATA_DIR}/installs/bun/1.2.14/bin:${MISE_DATA_DIR}/installs/go/1.24.3/bin:${RUSTUP_HOME}/toolchains/1.88.0-x86_64-unknown-linux-gnu/bin:${MISE_DATA_DIR}/installs/ruby/3.3.8/bin:${NVM_DIR}/versions/node/v20.0.0/bin:${PYENV_ROOT}/versions/3.11/bin"

  [[ -r $runtime_profile ]] || fail 'standalone Setup did not persist the Runtime profile'
  profile_path=$(PATH=/usr/bin:/bin /bin/bash --noprofile --norc -c 'source "$1"; source "$1"; printf "%s" "$PATH"' oma-profile "$runtime_profile")
  [[ $profile_path == "${expected_path}:"* ]] || fail 'persisted profile did not select all explicit Runtime paths'
  [[ $(grep -Fo "${PYENV_ROOT}/versions/3.11/bin" <<<"$profile_path" | wc -l | tr -d ' ') == 1 ]] || fail 'persisted profile duplicated Runtime paths'
  java_home=$(PATH=/usr/bin:/bin /bin/bash --noprofile --norc -c 'source "$1"; printf "%s" "$JAVA_HOME"' oma-profile "$runtime_profile")
  [[ $java_home == "${MISE_DATA_DIR}/installs/java/17.0.15" ]] || fail 'persisted profile did not select JAVA_HOME'
}

test_unavailable_version_fails_without_network
test_provider_selector_is_scrubbed_for_direct_setup
test_defaults_and_idempotency
test_empty_selectors_use_defaults
test_explicit_installed_versions
test_profile_persists_explicit_runtimes

printf 'runtime setup contract: PASS\n'
