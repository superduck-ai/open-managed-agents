#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
GENERATOR="$REPO_ROOT/scripts/generate-go.sh"
MAPPER_DIRECTORY="$REPO_ROOT/internal/db"
ORPHAN="$MAPPER_DIRECTORY/deleted_mapper_orphan.sqlmap.gen.go"

cleanup() {
  rm -f -- "$ORPHAN"
}
trap cleanup EXIT

generated_files() {
  find "$MAPPER_DIRECTORY" -type f -name '*.sqlmap.gen.go' | wc -l | tr -d ' '
}

generated_digest() {
  find "$MAPPER_DIRECTORY" -type f -name '*.sqlmap.gen.go' |
    sort |
    xargs shasum -a 256 |
    shasum -a 256 |
    awk '{print $1}'
}

expected_count() {
  grep -h 'go:generate' "$MAPPER_DIRECTORY"/*_mapper.go |
    grep -oE '\-out [^ ]+' |
    sed 's|^-out ||; s|^\./||' |
    sort -u |
    wc -l |
    tr -d ' '
}

"$GENERATOR" >/dev/null
expected="$(expected_count)"
expected_digest="$(generated_digest)"

# 失败场景优先：产物齐备时重跑，生成期间工作区不得残缺。删除仍被 go:generate
# 声明的产物会让 internal/db 在整个生成期间少文件，并发调用时后启动的删除就会
# 抹掉前一个进程已生成的文件，使调用方在退出码为 0 的情况下拿到残缺的目录。
# 这里全程采样文件数，不靠固定延时错峰两次调用，因此与机器快慢无关。
minimum_seen="$expected"
"$GENERATOR" >/dev/null &
generator_pid=$!
while kill -0 "$generator_pid" 2>/dev/null; do
  current="$(generated_files)"
  if [[ "$current" -lt "$minimum_seen" ]]; then
    minimum_seen="$current"
  fi
  sleep 0.05
done
wait "$generator_pid"

if [[ "$minimum_seen" != "$expected" ]]; then
  echo "generate dropped to $minimum_seen generated files mid-run, want $expected throughout" >&2
  exit 1
fi

# 成功场景：不再被 go:generate 声明的孤儿产物仍必须被清理。
printf '%s\n' 'package db' >"$ORPHAN"
"$GENERATOR" >/dev/null

if [[ -e "$ORPHAN" ]]; then
  echo "generator kept orphan generated file $ORPHAN" >&2
  exit 1
fi

if [[ "$(generated_files)" != "$expected" ]]; then
  echo "generator left $(generated_files) generated files, want $expected" >&2
  exit 1
fi

# 幂等：重复生成不得改动任何产物内容。
if [[ "$(generated_digest)" != "$expected_digest" ]]; then
  echo "regenerated artifacts differ from the first run" >&2
  exit 1
fi

echo "generate-go tests passed"
