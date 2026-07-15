#!/usr/bin/env bash

set -euo pipefail

oma_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
dockerfile="${oma_dir}/Dockerfile"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

[[ -f $dockerfile ]] || fail 'Dockerfile is missing'
[[ -x ${oma_dir}/entrypoint.sh ]] || fail 'entrypoint.sh is not executable'
[[ -x ${oma_dir}/setup_runtime.sh ]] || fail 'setup_runtime.sh is not executable'

bash -n "${oma_dir}/entrypoint.sh"
bash -n "${oma_dir}/setup_runtime.sh"

digest_re='@sha256:[0-9a-f]{64}'
[[ $(grep -Ec "$digest_re" "$dockerfile") -ge 2 ]] ||
  fail 'base and donor images must both use immutable digests'
donor_from='FROM --platform=linux/amd64 ${DONOR_REPOSITORY}@sha256:37316cf888deea15ca9995abfb3867c8dff24f043a21a7c766fff0e42913753a AS donor'
grep -Fx "$donor_from" "$dockerfile" >/dev/null ||
  fail 'donor FROM must append the required immutable digest after the repository override'
if grep -F 'ARG DONOR_IMAGE' "$dockerfile" >/dev/null; then
  fail 'the complete donor image reference must not be build-argument controlled'
fi
grep -F -- '--platform=linux/amd64' "$dockerfile" >/dev/null ||
  fail 'image does not constrain its final platform to Linux AMD64'
grep -F 'org.opencontainers.image.base.digest' "$dockerfile" >/dev/null ||
  fail 'base image digest provenance label is missing'
grep -F 'codex-universal-image-sbom.spdx.json' "$dockerfile" >/dev/null ||
  fail 'upstream SBOM is not retained in the image'
grep -F 'https://mirrors.tuna.tsinghua.edu.cn/ubuntu' "$dockerfile" >/dev/null ||
  fail 'Tsinghua apt mirror is not configured'
grep -F 'https://pypi.tuna.tsinghua.edu.cn/simple' "$dockerfile" >/dev/null ||
  fail 'Tsinghua PyPI mirror is not configured'
if grep -F 'trusted-host' "$dockerfile" >/dev/null; then
  fail 'PyPI mirror must retain normal TLS certificate verification'
fi
grep -F '[ ! -r /etc/profile.d/oma-runtime.sh ] || . /etc/profile.d/oma-runtime.sh' "$dockerfile" >/dev/null ||
  fail 'login shells do not reload the persisted OMA Runtime profile after manager hooks'
grep -F 'eval "$(mise activate bash --shims)"' "$dockerfile" >/dev/null ||
  fail 'Dockerfile must keep mise shims without enabling workspace chpwd hooks'
grep -F 'exec /bin/bash --login' "${oma_dir}/entrypoint.sh" >/dev/null ||
  fail 'empty entrypoint does not enter a login shell'
grep -F 'oma_entrypoint /etc/profile.d/oma-runtime.sh "$@"' "${oma_dir}/entrypoint.sh" >/dev/null ||
  fail 'production entrypoint must always use the fixed OMA Runtime profile'
grep -F 'oma_setup_runtime /etc/profile.d/oma-runtime.sh' "${oma_dir}/setup_runtime.sh" >/dev/null ||
  fail 'standalone Runtime Setup must always use the fixed OMA Runtime profile'
if grep -E 'OMA_TEST_(MODE|RUNTIME_PROFILE)' "${oma_dir}/entrypoint.sh" "${oma_dir}/setup_runtime.sh" >/dev/null; then
  fail 'production Runtime scripts must not expose a test environment profile override'
fi
if grep -F 'exec /bin/bash -i' "${oma_dir}/entrypoint.sh" >/dev/null; then
  fail 'empty entrypoint must not replace the login shell with a non-login shell'
fi

grep -F '${CARGO_HOME}/bin' "${oma_dir}/setup_runtime.sh" >/dev/null ||
  fail 'Cargo shims must honor the configurable Cargo home'
grep -F '${MISE_DATA_DIR}/shims' "${oma_dir}/setup_runtime.sh" >/dev/null ||
  fail 'mise shims must honor the configurable mise data directory'

for runtime in PYTHON NODE RUBY RUST GO BUN PHP JAVA SWIFT; do
  grep -F "ENV_${runtime}_VERSION" "${oma_dir}/setup_runtime.sh" >/dev/null ||
    fail "ENV_${runtime}_VERSION is not supported"
done

forbidden=CODE"X_ENV_"
if grep -R -F "$forbidden" "${oma_dir}/entrypoint.sh" "${oma_dir}/setup_runtime.sh" >/dev/null; then
  fail 'runtime scripts contain a provider-prefixed selector'
fi

printf 'sandbox source contract: PASS\n'
