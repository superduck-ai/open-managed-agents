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

pyenv_root="${tmp_dir}/pyenv"
nvm_dir="${tmp_dir}/nvm"
phpenv_root="${tmp_dir}/phpenv"
swiftly_bin_dir="${tmp_dir}/swiftly"
rustup_home="${tmp_dir}/rustup"
mise_data_dir="${tmp_dir}/mise"
runtime_profile="${tmp_dir}/profile.d/oma-runtime.sh"

link_runtime() {
  local bin_dir=$1
  local tool=$2
  mkdir -p "$bin_dir"
  ln -s "${test_dir}/fake-runtime-tool" "${bin_dir}/${tool}"
}

link_runtime "${pyenv_root}/versions/3.12/bin" python
link_runtime "${nvm_dir}/versions/node/v22.0.0/bin" node
link_runtime "${mise_data_dir}/installs/ruby/3.4.4/bin" ruby
link_runtime "${rustup_home}/toolchains/1.89.0-x86_64-unknown-linux-gnu/bin" rustc
link_runtime "${mise_data_dir}/installs/go/1.25.1/bin" go
link_runtime "${mise_data_dir}/installs/bun/1.2.14/bin" bun
link_runtime "${phpenv_root}/versions/8.4snapshot/bin" php
link_runtime "${mise_data_dir}/installs/java/21.0.8/bin" java
link_runtime "$swiftly_bin_dir" swift

forbidden_prefix=CODE"X"
forbidden_name="${forbidden_prefix}_ENV_NODE_VERSION"
output=$(
  env \
    OMA_TEST_STATE_DIR="$state_dir" \
    PATH="${fake_bin}:/usr/bin:/bin" \
    PYENV_ROOT="$pyenv_root" \
    NVM_DIR="$nvm_dir" \
    PHPENV_ROOT="$phpenv_root" \
    SWIFTLY_BIN_DIR="$swiftly_bin_dir" \
    CARGO_HOME="${tmp_dir}/cargo" \
    RUSTUP_HOME="$rustup_home" \
    MISE_DATA_DIR="$mise_data_dir" \
    OMA_TEST_REQUIRE_LOCAL_GO_TOOLCHAIN=1 \
    OMA_TEST_REQUIRE_MISE_OFFLINE=1 \
    OMA_TEST_REQUIRE_MISE_NO_CONFIG=1 \
    "$forbidden_name=20" \
    /bin/bash -c 'source "$1"; shift; oma_entrypoint "$@"' \
    oma-entrypoint-test "$oma_dir/entrypoint.sh" "$runtime_profile" /usr/bin/env \
    2>"${tmp_dir}/entrypoint.err"
)

grep -F 'OMA Sandbox runtime ready.' <<<"$output" >/dev/null || {
  printf 'FAIL: entrypoint did not emit its neutral readiness message\n' >&2
  exit 1
}
if grep -F "${forbidden_name}=" <<<"$output" >/dev/null; then
  printf 'FAIL: entrypoint passed an unsupported vendor selector to the command\n' >&2
  exit 1
fi
if grep -E 'OMA_(MISE_)?RUNTIME_(PATH|PROFILE)=' <<<"$output" >/dev/null; then
  printf 'FAIL: entrypoint leaked its internal concrete Runtime path variable\n' >&2
  exit 1
fi
expected_runtime_path="${swiftly_bin_dir}:${mise_data_dir}/installs/java/21.0.8/bin:${phpenv_root}/versions/8.4snapshot/bin:${mise_data_dir}/installs/bun/1.2.14/bin:${mise_data_dir}/installs/go/1.25.1/bin:${rustup_home}/toolchains/1.89.0-x86_64-unknown-linux-gnu/bin:${mise_data_dir}/installs/ruby/3.4.4/bin:${nvm_dir}/versions/node/v22.0.0/bin:${pyenv_root}/versions/3.12/bin"
entrypoint_path=$(sed -n 's/^PATH=//p' <<<"$output")
[[ $entrypoint_path == "${expected_runtime_path}:"* ]] || {
  printf 'FAIL: entrypoint did not keep concrete Runtime paths ahead of manager shims\n' >&2
  exit 1
}
[[ $(grep -Fo "${pyenv_root}/versions/3.12/bin" <<<"$entrypoint_path" | wc -l | tr -d ' ') == 1 ]] || {
  printf 'FAIL: entrypoint duplicated concrete Runtime paths\n' >&2
  exit 1
}
grep -Fx "JAVA_HOME=${mise_data_dir}/installs/java/21.0.8" <<<"$output" >/dev/null || {
  printf 'FAIL: entrypoint did not persist the selected JAVA_HOME\n' >&2
  exit 1
}
for runtime in python node ruby rust go bun php java swift; do
  [[ -s "${state_dir}/${runtime}" ]] || {
    printf 'FAIL: entrypoint did not activate %s\n' "$runtime" >&2
    exit 1
  }
done

printf 'entrypoint contract: PASS\n'
