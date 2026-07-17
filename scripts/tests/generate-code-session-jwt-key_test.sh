#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GENERATOR="$REPO_ROOT/scripts/generate-code-session-jwt-key.sh"
TEST_DIRECTORY="$(mktemp -d)"

cleanup() {
  rm -rf -- "$TEST_DIRECTORY"
}
trap cleanup EXIT

file_mode() {
  if stat -f '%Lp' "$1" >/dev/null 2>&1; then
    stat -f '%Lp' "$1"
    return
  fi
  stat -c '%a' "$1"
}

# 失败场景优先：已有文件必须保持原样，不能因误操作发生签名密钥轮换。
existing_key="$TEST_DIRECTORY/existing.pem"
printf '%s\n' 'keep-existing-key' >"$existing_key"
if "$GENERATOR" "$existing_key" >/dev/null 2>&1; then
  echo "expected generator to reject an existing output file" >&2
  exit 1
fi
if [[ "$(<"$existing_key")" != "keep-existing-key" ]]; then
  echo "generator changed an existing output file" >&2
  exit 1
fi

# 成功场景：生成结果应是可解析的 Ed25519 PKCS#8 私钥，并且仅 owner 可读写。
generated_key="$TEST_DIRECTORY/generated.pem"
"$GENERATOR" "$generated_key" >/dev/null
openssl pkey -in "$generated_key" -check -noout >/dev/null

if [[ "$(head -n 1 "$generated_key")" != "-----BEGIN PRIVATE KEY-----" ]]; then
  echo "generated key is not PKCS#8 PEM" >&2
  exit 1
fi

if [[ "$(file_mode "$generated_key")" != "600" ]]; then
  echo "generated key mode is $(file_mode "$generated_key"), want 600" >&2
  exit 1
fi

if ! openssl pkey -in "$generated_key" -text -noout 2>/dev/null | grep -qi 'ED25519'; then
  echo "generated key is not Ed25519" >&2
  exit 1
fi

echo "generate-code-session-jwt-key tests passed"
