#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_directory/.." && pwd)"
preset_file="$repo_root/internal/config/model-provider-presets.json"
output_file=""
dry_run=false

usage() {
  cat <<'HELP'
用法：./scripts/init-model-provider-presets.sh [--dry-run] [输出文件]

默认输出：当前工作目录下的 config/model-provider-presets.json
--dry-run 仅输出预置 JSON，不创建文件或目录。
已有目标（包括符号链接）会保留，重复执行不会覆盖已获取的模型或公司供应商。
目录不包含密钥；创建连接时请在控制台填写对应 API 或 Coding Plan 的密钥。

示例：
  ./scripts/init-model-provider-presets.sh --dry-run
  ./scripts/init-model-provider-presets.sh config/model-provider-presets.json

在服务 YAML 中配置（路径相对于该 YAML）：
  llm_provider_presets_file: ./model-provider-presets.json
HELP
}

for argument in "$@"; do
  case "$argument" in
    --dry-run) dry_run=true ;;
    --help|-h) usage; exit 0 ;;
    -*) usage >&2; exit 2 ;;
    *)
      if [[ -n "$output_file" ]]; then
        usage >&2
        exit 2
      fi
      output_file="$argument"
      ;;
  esac
done

if "$dry_run"; then
  cat "$preset_file"
  exit 0
fi

output_file="${output_file:-config/model-provider-presets.json}"
if [[ -e "$output_file" || -L "$output_file" ]]; then
  echo "已保留现有供应商目录：$output_file"
  exit 0
fi

output_directory="$(dirname -- "$output_file")"
mkdir -p -- "$output_directory"
temporary_file="$(mktemp "$output_directory/.model-provider-presets.XXXXXX")"
cleanup() {
  rm -f -- "$temporary_file"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

cat "$preset_file" >"$temporary_file"
chmod 0644 "$temporary_file"
if ! ln -- "$temporary_file" "$output_file" 2>/dev/null; then
  if [[ -e "$output_file" || -L "$output_file" ]]; then
    echo "已保留并发创建的供应商目录：$output_file"
    exit 0
  fi
  echo "无法创建供应商目录：$output_file" >&2
  exit 1
fi

echo "已初始化供应商目录：$output_file"
echo "请在服务 YAML 中设置 llm_provider_presets_file，路径相对于该 YAML。"
