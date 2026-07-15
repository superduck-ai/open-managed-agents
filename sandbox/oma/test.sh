#!/usr/bin/env bash

set -euo pipefail

oma_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

"${oma_dir}/tests/source_contract_test.sh"
"${oma_dir}/tests/runtime_setup_test.sh"
"${oma_dir}/tests/entrypoint_test.sh"

if [[ -n ${OMA_SANDBOX_IMAGE:-} ]]; then
  "${oma_dir}/tests/image_contract_test.sh" "$OMA_SANDBOX_IMAGE"
fi
