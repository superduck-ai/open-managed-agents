#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=../images/sandbox-base/versions.env
source "$ROOT_DIR/images/sandbox-base/versions.env"

# OCI/Docker use this DiffID for a filesystem layer whose uncompressed tar is
# empty. Such a layer legitimately appears as size zero in `docker history`.
EMPTY_TAR_DIFF_ID="sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef"

metadata() {
  printf '%s\n' \
    "platform=$SANDBOX_PLATFORM" \
    "target_image_size_bytes=$TARGET_IMAGE_SIZE_BYTES" \
    "ubuntu_digest=$UBUNTU_DIGEST" \
    "codex_universal_recipe_revision=$CODEX_UNIVERSAL_RECIPE_REVISION" \
    "environment_manager_revision=$ENVIRONMENT_MANAGER_REVISION" \
    "environment_manager_version=$ENVIRONMENT_MANAGER_VERSION" \
    "environment_manager_sha256=$ENVIRONMENT_MANAGER_SHA256" \
    "python=$PYTHON_VERSION" \
    "node=$NODE_VERSION" \
    "go=$GO_VERSION" \
    "java=$JAVA_VERSION" \
    "php=$PHP_VERSION" \
    "gcc=$GCC_VERSION" \
    "bun=$BUN_VERSION" \
    "rust=$RUST_VERSION" \
    "ruby=$RUBY_VERSION" \
    "claude=$CLAUDE_VERSION"
}

require_literal() {
  local file="$1"
  local literal="$2"

  grep -Fq -- "$literal" "$file" || {
    printf 'source contract failed: %s does not contain %s\n' "$file" "$literal" >&2
    return 1
  }
}

reject_literal() {
  local file="$1"
  local literal="$2"

  if grep -Fq -- "$literal" "$file"; then
    printf 'source contract failed: %s contains prohibited text %s\n' "$file" "$literal" >&2
    return 1
  fi
}

verify_sha256_value() {
  local name="$1"
  local value="${!name:-}"

  [[ "$value" =~ ^[0-9a-f]{64}$ ]] || {
    printf 'source contract failed: %s is not a SHA-256\n' "$name" >&2
    return 1
  }
}

verify_curl_retry_policy() {
  local file="$1"
  local curl_count retry_count

  curl_count="$(grep -Ec '(^[[:space:]]*|RUN[[:space:]]+|&&[[:space:]]+)curl[[:space:]]+-' "$file" || true)"
  retry_count="$(grep -Ec 'curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15([[:space:]]|$)' "$file" || true)"
  [[ "$curl_count" -gt 0 && "$retry_count" == "$curl_count" ]] || {
    printf 'source contract failed: %s has %s curl downloads but %s complete retry policies\n' \
      "$file" "$curl_count" "$retry_count" >&2
    return 1
  }
}

verify_source() {
  local dockerfile="$ROOT_DIR/images/sandbox-base/Dockerfile"
  local verifier="$ROOT_DIR/images/sandbox-base/verify-contract.sh"
  local workflow="$ROOT_DIR/.github/workflows/sandbox-base-image.yml"

  [[ -f "$dockerfile" ]] || {
    printf 'source contract failed: missing %s\n' "$dockerfile" >&2
    return 1
  }
  [[ -f "$verifier" ]] || {
    printf 'source contract failed: missing %s\n' "$verifier" >&2
    return 1
  }
  [[ -f "$workflow" ]] || {
    printf 'source contract failed: missing %s\n' "$workflow" >&2
    return 1
  }

  require_literal "$dockerfile" 'ARG SANDBOX_PLATFORM=linux/amd64'
  require_literal "$dockerfile" "ubuntu@${UBUNTU_DIGEST} AS base"
  require_literal "$dockerfile" 'FROM environment_manager AS environment_manager_source'
  require_literal "$dockerfile" 'USER root'
  require_literal "$dockerfile" 'ENV HOME=/root'
  require_literal "$dockerfile" 'WORKDIR /home/user'
  require_literal "$dockerfile" '/root/.claude.json'
  require_literal "$dockerfile" 'COPY config/sudoers-claude /etc/sudoers.d/claude'
  require_literal "$dockerfile" 'useradd --uid 1001 --gid claude --create-home --shell /bin/bash claude'
  require_literal "$dockerfile" 'usermod --append --groups claude user'
  require_literal "$dockerfile" 'PIP_CACHE_DIR=/home/claude/.cache/pip'
  require_literal "$dockerfile" 'NODE_PATH=/home/claude/.npm-global/lib/node_modules'
  require_literal "$dockerfile" 'PATH=/home/claude/.npm-global/bin:/home/claude/.local/bin:'
  require_literal "$dockerfile" 'GEMRC=/home/user/.gemrc'
  require_literal "$dockerfile" 'MAVEN_ARGS="-s /home/user/.m2/settings.xml"'
  require_literal "$dockerfile" 'GRADLE_USER_HOME=/home/user/.gradle'
  require_literal "$dockerfile" 'COMPOSER_HOME=/home/user/.config/composer'
  require_literal "$dockerfile" 'COPY --from=environment_manager_source /environment-manager /opt/env-runner/environment-manager'
  require_literal "$dockerfile" 'ln -s /opt/env-runner/environment-manager /usr/local/bin/environment-manager'
  require_literal "$dockerfile" 'ln -s /opt/claude-code/bin/claude /usr/local/bin/claude'
  require_literal "$dockerfile" 'ln -sf /usr/bin/gcc-14 /usr/local/bin/cc'
  require_literal "$dockerfile" 'ln -sf /usr/bin/g++-14 /usr/local/bin/c++'
  require_literal "$dockerfile" 'ENVIRONMENT_MANAGER_SHA256'
  require_literal "$dockerfile" 'ENVIRONMENT_MANAGER_VERSION'
  require_literal "$dockerfile" 'APT::Keep-Downloaded-Packages "true"'
  require_literal "$dockerfile" '--components="rustc,rust-std-x86_64-unknown-linux-gnu,cargo,rustfmt-preview,clippy-preview,rust-analyzer-preview,llvm-tools-preview"'
  require_literal "$dockerfile" 'COPY config/profile.sh /etc/profile.d/oma-sandbox.sh'
  require_literal "$verifier" 'assert_absent codex'
  require_literal "$verifier" 'assert_absent dockerd'
  require_literal "$verifier" 'assert_writable_dir /root/.claude'
  require_literal "$verifier" 'getent passwd claude'
  require_literal "$verifier" 'assert_writable_dir "$shared_dir"'
  require_literal "$verifier" "assert_output_equals '/opt/env-runner/environment-manager' readlink /usr/local/bin/environment-manager"
  require_literal "$verifier" "assert_output_equals '2.1.120 (Claude Code)'"
  require_literal "$verifier" 'assert_cargo_builds'
  require_literal "$verifier" 'task-run --help'
  reject_literal "$dockerfile" 'python -m pip install --no-cache-dir --upgrade pip'
  reject_literal "$dockerfile" 'gem install --no-document bundler'
  reject_literal "$dockerfile" 'PIP_CONFIG_FILE=/root/'
  reject_literal "$dockerfile" 'trusted-host'
  reject_literal "$dockerfile" 'GOSUMDB=off'
  verify_curl_retry_policy "$dockerfile"
  verify_curl_retry_policy "$workflow"

  local final_user final_workdir
  final_user="$(awk 'toupper($1) == "USER" { value = $2 } END { print value }' "$dockerfile")"
  final_workdir="$(awk 'toupper($1) == "WORKDIR" { value = $2 } END { print value }' "$dockerfile")"
  [[ "$final_user" == "root" ]] || {
    printf 'source contract failed: final Dockerfile USER is %s, expected root\n' "$final_user" >&2
    return 1
  }
  [[ "$final_workdir" == "/home/user" ]] || {
    printf 'source contract failed: final Dockerfile WORKDIR is %s, expected /home/user\n' "$final_workdir" >&2
    return 1
  }

  local checksum_name
  for checksum_name in \
    PYTHON_SHA256 NODE_SHA256 GO_SHA256 JAVA_SHA256 PHP_SHA256 BUN_SHA256 \
    RUST_SHA256 RUBY_SHA256 CLAUDE_SHA256 UV_SHA256 GRADLE_SHA256 DOCKER_CLI_SHA256; do
    verify_sha256_value "$checksum_name"
  done

  [[ "$MAVEN_SHA512" =~ ^[0-9a-f]{128}$ ]] || {
    printf 'source contract failed: MAVEN_SHA512 is not a SHA-512\n' >&2
    return 1
  }
  [[ "$COMPOSER_INSTALLER_SHA384" =~ ^[0-9a-f]{96}$ ]] || {
    printf 'source contract failed: COMPOSER_INSTALLER_SHA384 is not a SHA-384\n' >&2
    return 1
  }
  [[ "$CODEX_UNIVERSAL_RECIPE_REVISION" =~ ^[0-9a-f]{40}$ ]] || {
    printf 'source contract failed: CODEX_UNIVERSAL_RECIPE_REVISION is not a Git commit\n' >&2
    return 1
  }
  [[ "$ENVIRONMENT_MANAGER_REVISION" =~ ^[0-9a-f]{40}$ ]] || {
    printf 'source contract failed: ENVIRONMENT_MANAGER_REVISION is not a Git commit\n' >&2
    return 1
  }
  [[ "$ENVIRONMENT_MANAGER_VERSION" =~ ^[0-9a-f]{7}$ ]] || {
    printf 'source contract failed: ENVIRONMENT_MANAGER_VERSION is invalid\n' >&2
    return 1
  }
  [[ "$TARGET_IMAGE_SIZE_BYTES" =~ ^[1-9][0-9]*$ ]] || {
    printf 'source contract failed: TARGET_IMAGE_SIZE_BYTES is not a positive integer\n' >&2
    return 1
  }
  verify_sha256_value ENVIRONMENT_MANAGER_SHA256

  printf 'sandbox image source contract passed\n'
}

check_dockerfile() (
  local manager_context
  manager_context="$(mktemp -d)"
  trap 'rm -rf "$manager_context"' EXIT
  : > "$manager_context/environment-manager"

  docker buildx build \
    --check \
    --platform "$SANDBOX_PLATFORM" \
    --build-context "environment_manager=$manager_context" \
    "$ROOT_DIR/images/sandbox-base"
)

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

build_image() (
  local manager_binary="${ENVIRONMENT_MANAGER_BINARY:-}"
  local manager_sha256="$ENVIRONMENT_MANAGER_SHA256"
  local image_tag="${SANDBOX_IMAGE_TAG:-oma/managed-agent-sandbox:latest}"
  local image_output="${SANDBOX_IMAGE_OUTPUT:-load}"
  local metadata_file="${SANDBOX_IMAGE_METADATA_FILE:-}"
  local cache_from="${SANDBOX_BUILDX_CACHE_FROM:-}"
  local cache_to="${SANDBOX_BUILDX_CACHE_TO:-}"

  [[ -n "$manager_binary" ]] || {
    printf 'ENVIRONMENT_MANAGER_BINARY is required\n' >&2
    return 2
  }
  [[ -f "$manager_binary" ]] || {
    printf 'environment-manager binary not found: %s\n' "$manager_binary" >&2
    return 2
  }
  [[ "$(sha256_file "$manager_binary")" == "$manager_sha256" ]] || {
    printf 'environment-manager checksum mismatch; expected pinned %s\n' "$manager_sha256" >&2
    return 2
  }
  [[ "$image_output" == "load" || "$image_output" == "push" ]] || {
    printf 'SANDBOX_IMAGE_OUTPUT must be load or push\n' >&2
    return 2
  }

  verify_source

  local manager_context
  manager_context="$(mktemp -d)"
  trap 'rm -rf "$manager_context"' EXIT
  install -m 0755 "$manager_binary" "$manager_context/environment-manager"

  local output_flag="--load"
  if [[ "$image_output" == "push" ]]; then
    output_flag="--push"
  fi

  local -a build_options=(--provenance=mode=min)
  if [[ -n "$metadata_file" ]]; then
    mkdir -p "$(dirname "$metadata_file")"
    build_options+=(--metadata-file "$metadata_file")
  fi
  if [[ -n "$cache_from" ]]; then
    build_options+=(--cache-from "$cache_from")
  fi
  if [[ -n "$cache_to" ]]; then
    build_options+=(--cache-to "$cache_to")
  fi

  docker buildx build \
    --platform "$SANDBOX_PLATFORM" \
    --build-context "environment_manager=$manager_context" \
    --build-arg "SANDBOX_PLATFORM=$SANDBOX_PLATFORM" \
    --build-arg "PYTHON_VERSION=$PYTHON_VERSION" \
    --build-arg "PYTHON_SHA256=$PYTHON_SHA256" \
    --build-arg "NODE_VERSION=$NODE_VERSION" \
    --build-arg "NODE_SHA256=$NODE_SHA256" \
    --build-arg "GO_VERSION=$GO_VERSION" \
    --build-arg "GO_SHA256=$GO_SHA256" \
    --build-arg "JAVA_VERSION=$JAVA_VERSION" \
    --build-arg "JAVA_SHA256=$JAVA_SHA256" \
    --build-arg "PHP_VERSION=$PHP_VERSION" \
    --build-arg "PHP_SHA256=$PHP_SHA256" \
    --build-arg "BUN_VERSION=$BUN_VERSION" \
    --build-arg "BUN_SHA256=$BUN_SHA256" \
    --build-arg "RUST_VERSION=$RUST_VERSION" \
    --build-arg "RUST_SHA256=$RUST_SHA256" \
    --build-arg "RUBY_VERSION=$RUBY_VERSION" \
    --build-arg "RUBY_SHA256=$RUBY_SHA256" \
    --build-arg "CLAUDE_VERSION=$CLAUDE_VERSION" \
    --build-arg "CLAUDE_SHA256=$CLAUDE_SHA256" \
    --build-arg "UV_VERSION=$UV_VERSION" \
    --build-arg "UV_SHA256=$UV_SHA256" \
    --build-arg "YARN_VERSION=$YARN_VERSION" \
    --build-arg "PNPM_VERSION=$PNPM_VERSION" \
    --build-arg "MAVEN_VERSION=$MAVEN_VERSION" \
    --build-arg "MAVEN_SHA512=$MAVEN_SHA512" \
    --build-arg "GRADLE_VERSION=$GRADLE_VERSION" \
    --build-arg "GRADLE_SHA256=$GRADLE_SHA256" \
    --build-arg "COMPOSER_VERSION=$COMPOSER_VERSION" \
    --build-arg "COMPOSER_INSTALLER_SHA384=$COMPOSER_INSTALLER_SHA384" \
    --build-arg "DOCKER_CLI_VERSION=$DOCKER_CLI_VERSION" \
    --build-arg "DOCKER_CLI_SHA256=$DOCKER_CLI_SHA256" \
    --build-arg "ENVIRONMENT_MANAGER_SHA256=$manager_sha256" \
    --build-arg "ENVIRONMENT_MANAGER_REVISION=$ENVIRONMENT_MANAGER_REVISION" \
    --build-arg "ENVIRONMENT_MANAGER_VERSION=$ENVIRONMENT_MANAGER_VERSION" \
    --tag "$image_tag" \
    "$output_flag" \
    "${build_options[@]}" \
    "$ROOT_DIR/images/sandbox-base"
)

test_image() {
  local image_ref="${1:-}"

  [[ -n "$image_ref" ]] || {
    printf 'image reference is required\n' >&2
    return 2
  }

  local image_os image_arch storage_size uncompressed_size image_id size_target_status
  local rootfs_layer_count rootfs_empty_layer_markers rootfs_empty_layer_count
  local expected_nonempty_layer_count history_entry_count history_nonempty_layer_count
  local empty_layer_template layer_sizes confirmed_layer_sizes
  local expected_verifier_sha image_verifier_output image_verifier_sha
  image_os="$(docker image inspect --format '{{.Os}}' "$image_ref")"
  image_arch="$(docker image inspect --format '{{.Architecture}}' "$image_ref")"
  storage_size="$(docker image inspect --format '{{.Size}}' "$image_ref")"
  image_id="$(docker image inspect --format '{{.Id}}' "$image_ref")"
  rootfs_layer_count="$(docker image inspect --format '{{len .RootFS.Layers}}' "$image_ref")"
  empty_layer_template="{{range .RootFS.Layers}}{{if eq . \"$EMPTY_TAR_DIFF_ID\"}}1{{end}}{{end}}"
  rootfs_empty_layer_markers="$(docker image inspect --format "$empty_layer_template" "$image_ref")"

  [[ "$image_os/$image_arch" == "$SANDBOX_PLATFORM" ]] || {
    printf 'image platform mismatch: expected %s, got %s/%s\n' "$SANDBOX_PLATFORM" "$image_os" "$image_arch" >&2
    return 1
  }
  [[ "$storage_size" =~ ^[0-9]+$ ]] || {
    printf 'docker returned a non-numeric storage size: %s\n' "$storage_size" >&2
    return 1
  }
  [[ "$rootfs_layer_count" =~ ^[1-9][0-9]*$ ]] || {
    printf 'docker returned an invalid RootFS layer count: %s\n' "$rootfs_layer_count" >&2
    return 1
  }
  [[ "$rootfs_empty_layer_markers" =~ ^1*$ ]] || {
    printf 'docker returned invalid empty RootFS layer markers: %s\n' "$rootfs_empty_layer_markers" >&2
    return 1
  }

  # Containerd-backed engines can report zero for newly loaded layer sizes
  # until the first mount unpacks the image. A no-op container makes that
  # metadata available without modifying the image or its runtime filesystem.
  docker run --rm --platform "$SANDBOX_PLATFORM" --entrypoint /bin/true "$image_ref"
  layer_sizes="$(docker image history --human=false --format '{{.Size}}' "$image_ref")"
  confirmed_layer_sizes="$(docker image history --human=false --format '{{.Size}}' "$image_ref")"
  [[ "$layer_sizes" == "$confirmed_layer_sizes" ]] || {
    printf 'docker image history changed between reads; refusing an unstable size result\n' >&2
    return 1
  }
  if [[ -z "$layer_sizes" ]] || grep -Eqv '^[0-9]+$' <<<"$layer_sizes"; then
    printf 'docker returned invalid uncompressed layer sizes\n' >&2
    return 1
  fi
  history_entry_count="$(awk 'END { print NR }' <<<"$layer_sizes")"
  history_nonempty_layer_count="$(awk '$1 > 0 { count++ } END { print count + 0 }' <<<"$layer_sizes")"
  rootfs_empty_layer_count="${#rootfs_empty_layer_markers}"
  expected_nonempty_layer_count=$((rootfs_layer_count - rootfs_empty_layer_count))
  ((history_nonempty_layer_count == expected_nonempty_layer_count)) || {
    printf 'docker returned incomplete or inconsistent image history: %s non-empty entries for %s non-empty RootFS layers\n' \
      "$history_nonempty_layer_count" "$expected_nonempty_layer_count" >&2
    return 1
  }
  uncompressed_size="$(awk '{sum += $1} END {printf "%.0f", sum}' <<<"$layer_sizes")"
  size_target_status="at_or_below_target"
  if ((uncompressed_size > TARGET_IMAGE_SIZE_BYTES)); then
    size_target_status="above_target"
  fi

  printf '%s\n' \
    "image=$image_ref" \
    "platform=$image_os/$image_arch" \
    "storage_size_bytes=$storage_size" \
    "uncompressed_size_bytes=$uncompressed_size" \
    "target_image_size_bytes=$TARGET_IMAGE_SIZE_BYTES" \
    "size_target_status=$size_target_status" \
    "rootfs_layers=$rootfs_layer_count" \
    "rootfs_empty_layers=$rootfs_empty_layer_count" \
    "history_entries=$history_entry_count" \
    "history_nonempty_layers=$history_nonempty_layer_count" \
    "image_id=$image_id"
  expected_verifier_sha="$(sha256_file "$ROOT_DIR/images/sandbox-base/verify-contract.sh")"
  image_verifier_output="$(docker run --rm --platform "$SANDBOX_PLATFORM" --entrypoint sha256sum \
    "$image_ref" /usr/local/bin/verify-sandbox-contract)"
  image_verifier_sha="${image_verifier_output%%[[:space:]]*}"
  [[ "$image_verifier_sha" == "$expected_verifier_sha" ]] || {
    printf 'image runtime verifier checksum mismatch: expected %s, got %s\n' \
      "$expected_verifier_sha" "$image_verifier_sha" >&2
    return 1
  }
  docker run --rm --platform "$SANDBOX_PLATFORM" "$image_ref" /usr/local/bin/verify-sandbox-contract
}

usage() {
  printf 'usage: %s <metadata|verify-source|check|build|test-image> [image]\n' "${0##*/}" >&2
}

main() {
  case "${1:-}" in
    metadata)
      metadata
      ;;
    verify-source)
      verify_source
      ;;
    check)
      check_dockerfile
      ;;
    build)
      build_image
      ;;
    test-image)
      test_image "${2:-}"
      ;;
    *)
      usage
      return 2
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
