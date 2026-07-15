#!/usr/bin/env bash

set -euo pipefail

oma_entrypoint() {
  local runtime_profile=$1
  shift
  local script_dir
  script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

  # Source the single Runtime Setup implementation so selector scrubbing and
  # activated manager paths remain in effect for the user command.
  # shellcheck disable=SC1091
  source "${script_dir}/setup_runtime.sh"
  oma_setup_runtime "$runtime_profile"

  printf 'OMA Sandbox runtime ready.\n'

  if [[ $# -eq 0 ]]; then
    exec /bin/bash --login
  fi

  # The image profile reloads the persisted OMA Runtime profile after upstream
  # manager hooks. Source it explicitly as well in the command shell, then
  # preserve user argv exactly.
  exec /bin/bash --login -c '
    runtime_profile=$1
    shift
    if [[ -r $runtime_profile ]]; then
      # shellcheck disable=SC1090
      source "$runtime_profile"
    fi
    unset OMA_RUNTIME_PROFILE
    exec "$@"
  ' oma-entrypoint "$runtime_profile" "$@"
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
  oma_entrypoint /etc/profile.d/oma-runtime.sh "$@"
fi
