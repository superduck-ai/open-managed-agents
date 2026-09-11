#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

mapper_directory="internal/db"

expected_outputs="$(mktemp)"
actual_outputs="$(mktemp)"
cleanup() {
  rm -f -- "$expected_outputs" "$actual_outputs"
}
trap cleanup EXIT

grep -h 'go:generate' "$mapper_directory"/*_mapper.go |
  grep -oE '\-out [^ ]+' |
  sed 's|^-out ||; s|^\./||' |
  sort -u >"$expected_outputs"

# 空集合会让下面把全部产物判定为孤儿。
if [[ ! -s "$expected_outputs" ]]; then
  echo "generate-go: no sqlmapgen -out targets found in $mapper_directory/*_mapper.go" >&2
  exit 1
fi

find "$mapper_directory" -type f -name '*.sqlmap.gen.go' |
  sed "s|^$mapper_directory/||" |
  sort -u >"$actual_outputs"

# 只删孤儿：删除仍被声明的产物会让 internal/db 在生成期间残缺，并发调用时互相抹除。
while IFS= read -r orphan; do
  [[ -n "$orphan" ]] || continue
  rm -f -- "$mapper_directory/$orphan"
done < <(comm -13 "$expected_outputs" "$actual_outputs")

go generate ./"$mapper_directory"
