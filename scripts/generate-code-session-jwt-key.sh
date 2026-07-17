#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
用法：
  ./scripts/generate-code-session-jwt-key.sh <私钥输出路径>

示例：
  ./scripts/generate-code-session-jwt-key.sh /var/lib/open-managed-agents/secrets/code-session-jwt-key.pem

脚本生成未加密的 Ed25519 PKCS#8 PEM 私钥，文件权限为 0600。
为避免意外轮换 JWT 签名密钥，如果目标路径已经存在，脚本会直接失败且不会覆盖。
EOF
}

if [[ $# -ne 1 ]]; then
  usage >&2
  exit 2
fi

if ! command -v openssl >/dev/null 2>&1; then
  echo "错误：未找到 openssl，请先安装 OpenSSL。" >&2
  exit 1
fi

output_file="$1"
output_directory="$(dirname -- "$output_file")"

if [[ ! -d "$output_directory" ]]; then
  echo "错误：输出目录不存在：$output_directory" >&2
  echo "请先创建并保护该目录，再重新运行脚本。" >&2
  exit 1
fi

# 即使目标是悬空符号链接，也必须拒绝继续，避免把私钥写入非预期位置。
if [[ -e "$output_file" || -L "$output_file" ]]; then
  echo "错误：目标路径已经存在，拒绝覆盖 JWT 签名私钥：$output_file" >&2
  exit 1
fi

# mktemp 在目标目录中创建临时文件，保证后面的硬链接发布不会跨文件系统。
# umask 和显式 chmod 共同确保生成过程及最终文件都只有 owner 可读写。
umask 077
temporary_file="$(mktemp "${output_file}.tmp.XXXXXX")"
cleanup() {
  rm -f -- "$temporary_file"
}
trap cleanup EXIT HUP INT TERM

openssl genpkey \
  -algorithm ED25519 \
  -out "$temporary_file"

# 在发布前让 OpenSSL 重新解析并校验私钥，避免留下截断、无效或错误算法的 Secret。
openssl pkey -in "$temporary_file" -check -noout >/dev/null
if ! openssl pkey -in "$temporary_file" -text -noout 2>/dev/null | grep -qi 'ED25519'; then
  echo "错误：OpenSSL 生成的私钥不是 Ed25519。" >&2
  exit 1
fi
chmod 0600 "$temporary_file"

# 使用硬链接完成原子、不可覆盖的发布：若并发任务先创建了目标，ln 会失败。
if ! ln -- "$temporary_file" "$output_file"; then
  echo "错误：无法创建私钥，目标可能已被并发创建：$output_file" >&2
  exit 1
fi

rm -f -- "$temporary_file"
trap - EXIT HUP INT TERM

echo "已生成 code session JWT 签名私钥：$output_file"
echo "请将 CODE_SESSION_JWT_SIGNING_KEY_FILE 指向该文件，并以只读方式提供给 oma-server。"
