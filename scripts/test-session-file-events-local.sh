#!/usr/bin/env bash
set -euo pipefail

API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:38080}"
ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-sk-ant-local-default}"
ANTHROPIC_VERSION="${ANTHROPIC_VERSION:-2023-06-01}"
ANTHROPIC_BETA="${ANTHROPIC_BETA:-files-api-2025-04-14, managed-agents-2026-04-01}"
MODEL="${MODEL:-claude-opus-4-6}"
HTTP_TIMEOUT_SECONDS="${HTTP_TIMEOUT_SECONDS:-30}"
WAIT_FOR_AGENT_SECONDS="${WAIT_FOR_AGENT_SECONDS:-180}"
POLL_INTERVAL_SECONDS="${POLL_INTERVAL_SECONDS:-3}"
SSE_TIMEOUT_SECONDS="${SSE_TIMEOUT_SECONDS:-15}"
CLEANUP="${CLEANUP:-1}"
RUN_NEGATIVE_CHECK="${RUN_NEGATIVE_CHECK:-1}"
SERVER_LOG_FILE="${SERVER_LOG_FILE:-}"
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
ARTIFACT_DIR="${ARTIFACT_DIR:-/tmp/open-managed-agents-session-file-e2e-$RUN_ID}"

AGENT_ID="${AGENT_ID:-}"
ENVIRONMENT_ID="${ENVIRONMENT_ID:-}"
SESSION_ID=""
FILE_ID=""
RESOURCE_ID=""
CREATED_AGENT=0
CREATED_ENVIRONMENT=0
SSE_PID=""
SERVER_LOG_PID=""
REQUEST_INDEX=0
RESPONSE_BODY=""
RESPONSE_HEADERS=""
RESPONSE_STATUS=""

usage() {
  cat <<'EOF'
真实本地服务 Session File Event 端到端测试。

用法：
  scripts/test-session-file-events-local.sh

常用环境变量：
  API_BASE_URL=http://127.0.0.1:38080
  ANTHROPIC_API_KEY=sk-ant-local-default
  MODEL=claude-opus-4-6
  TEST_FILE=/absolute/path/to/file.txt       # 不传则自动创建带唯一 marker 的文本文件
  EXPECTED_TEXT=expected-agent-reply-marker # 自定义 TEST_FILE 时建议设置
  MOUNT_PATH='/uploads/reference file.txt'
  WAIT_FOR_AGENT_SECONDS=180                 # 设为 0，只验证 API、SSE 和公开边界
  SERVER_LOG_FILE=/tmp/oma-server.log        # 实时打印该文件中新追加的服务日志
  CLEANUP=1                                  # 结束时删除 Session/File/Environment，归档 Agent
  RUN_NEGATIVE_CHECK=1                       # 挂载前先验证 events.send 返回 400
  ARTIFACT_DIR=/tmp/my-e2e-artifacts

建议这样启动服务并保留服务日志：
  CONFIG_FILE=/path/to/config.yaml go run . 2>&1 | tee /tmp/oma-server.log

然后执行：
  SERVER_LOG_FILE=/tmp/oma-server.log \
    scripts/test-session-file-events-local.sh

脚本会逐步打印：
  - 可复制的 curl 命令（API key 保留为环境变量，不泄漏明文）
  - HTTP response headers、状态码和格式化 JSON body
  - Session SSE 原始内容
  - 可选的服务端实时日志
  - 每项公开/私有数据边界断言
EOF
}

log() {
  printf '\n[%s] %s\n' "$(date '+%H:%M:%S')" "$*"
}

die() {
  printf '\n[FAIL] %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

print_common_headers() {
  local continue_command="${1:-0}"
  cat <<EOF
  -H "x-api-key: \$ANTHROPIC_API_KEY" \\
  -H 'anthropic-version: $ANTHROPIC_VERSION' \\
EOF
  if [[ "$continue_command" == '1' ]]; then
    printf '  -H %q \\\n' "anthropic-beta: $ANTHROPIC_BETA"
  else
    printf '  -H %q\n' "anthropic-beta: $ANTHROPIC_BETA"
  fi
}

print_json_curl() {
  local method="$1"
  local url="$2"
  local body="$3"
  printf 'curl --globoff -sS -X %q %q \\\n' "$method" "$url"
  if [[ -n "$body" ]]; then
    print_common_headers 1
    printf '  -H %q \\\n  --data-binary %q\n' 'content-type: application/json' "$body"
  else
    print_common_headers 0
  fi
}

pretty_print_body() {
  local file="$1"
  if [[ ! -s "$file" ]]; then
    echo '[response body] <empty>'
    return
  fi
  echo '[response body]'
  if jq . "$file" >/dev/null 2>&1; then
    jq . "$file"
  else
    sed 's/^/  /' "$file"
  fi
}

request_json() {
  local label="$1"
  local method="$2"
  local path="$3"
  local body="${4:-}"
  local expected_status="${5:-200}"
  local url="$API_BASE_URL$path"

  REQUEST_INDEX=$((REQUEST_INDEX + 1))
  RESPONSE_HEADERS="$ARTIFACT_DIR/$(printf '%02d' "$REQUEST_INDEX")-${label}.headers"
  RESPONSE_BODY="$ARTIFACT_DIR/$(printf '%02d' "$REQUEST_INDEX")-${label}.json"

  log "$label"
  echo '[curl command]'
  print_json_curl "$method" "$url" "$body"

  local curl_args=(
    --globoff --silent --show-error
    --connect-timeout 5 --max-time "$HTTP_TIMEOUT_SECONDS"
    --request "$method"
    --header "x-api-key: $ANTHROPIC_API_KEY"
    --header "anthropic-version: $ANTHROPIC_VERSION"
    --header "anthropic-beta: $ANTHROPIC_BETA"
    --dump-header "$RESPONSE_HEADERS"
    --output "$RESPONSE_BODY"
    --write-out '%{http_code}'
  )
  if [[ -n "$body" ]]; then
    curl_args+=(--header 'content-type: application/json' --data-binary "$body")
  fi

  if ! RESPONSE_STATUS="$(curl "${curl_args[@]}" "$url")"; then
    [[ -f "$RESPONSE_HEADERS" ]] && sed 's/^/[response header] /' "$RESPONSE_HEADERS" >&2
    [[ -f "$RESPONSE_BODY" ]] && pretty_print_body "$RESPONSE_BODY" >&2
    die "$label 请求失败"
  fi
  echo "[http status] $RESPONSE_STATUS"
  echo '[response headers]'
  sed 's/^/  /' "$RESPONSE_HEADERS"
  pretty_print_body "$RESPONSE_BODY"
  [[ "$RESPONSE_STATUS" == "$expected_status" ]] || die "${label} 状态码为 ${RESPONSE_STATUS}，预期 ${expected_status}"
}

upload_file() {
  local file="$1"
  local mime_type="$2"
  local url="$API_BASE_URL/v1/files?beta=true"

  REQUEST_INDEX=$((REQUEST_INDEX + 1))
  RESPONSE_HEADERS="$ARTIFACT_DIR/$(printf '%02d' "$REQUEST_INDEX")-upload-file.headers"
  RESPONSE_BODY="$ARTIFACT_DIR/$(printf '%02d' "$REQUEST_INDEX")-upload-file.json"

  log 'upload-file'
  echo '[curl command]'
  printf 'curl --globoff -sS -X POST %q \\\n' "$url"
  print_common_headers 1
  printf '  -F %q\n' "file=@$file;type=$mime_type"

  if ! RESPONSE_STATUS="$(curl --globoff --silent --show-error \
    --connect-timeout 5 --max-time "$HTTP_TIMEOUT_SECONDS" \
    --request POST \
    --header "x-api-key: $ANTHROPIC_API_KEY" \
    --header "anthropic-version: $ANTHROPIC_VERSION" \
    --header "anthropic-beta: $ANTHROPIC_BETA" \
    --form "file=@$file;type=$mime_type" \
    --dump-header "$RESPONSE_HEADERS" \
    --output "$RESPONSE_BODY" \
    --write-out '%{http_code}' \
    "$url")"; then
    die 'upload-file 请求失败'
  fi
  echo "[http status] $RESPONSE_STATUS"
  echo '[response headers]'
  sed 's/^/  /' "$RESPONSE_HEADERS"
  pretty_print_body "$RESPONSE_BODY"
  [[ "$RESPONSE_STATUS" == '200' ]] || die "upload-file 状态码为 ${RESPONSE_STATUS}，预期 200"
}

assert_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  grep -Fq -- "$needle" "$file" || die "${label}：未找到 ${needle}"
  echo "[PASS] $label"
}

assert_not_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  if grep -Fq -- "$needle" "$file"; then
    die "${label}：不应出现 ${needle}"
  fi
  echo "[PASS] $label"
}

start_server_log_tail() {
  if [[ -z "$SERVER_LOG_FILE" ]]; then
    log '未设置 SERVER_LOG_FILE；脚本仍会打印全部客户端请求/响应'
    echo '如需服务端日志：SERVER_LOG_FILE=/tmp/oma-server.log scripts/test-session-file-events-local.sh'
    return
  fi
  [[ -f "$SERVER_LOG_FILE" ]] || die "SERVER_LOG_FILE 不存在：$SERVER_LOG_FILE"
  log "实时跟踪服务日志：$SERVER_LOG_FILE"
  tail -n 0 -F "$SERVER_LOG_FILE" | sed 's/^/[server] /' &
  SERVER_LOG_PID=$!
}

start_sse() {
  local url="$API_BASE_URL/v1/sessions/$SESSION_ID/events/stream?beta=true"
  local output="$ARTIFACT_DIR/session-events.sse"
  log 'open-session-events-sse'
  echo '[curl command]'
  printf 'curl --globoff -sS -N --max-time %q %q \\\n' "$SSE_TIMEOUT_SECONDS" "$url"
  print_common_headers 1
  printf '  -H %q\n' 'accept: text/event-stream'
  : >"$output"
  curl --globoff --silent --show-error --no-buffer \
    --max-time "$SSE_TIMEOUT_SECONDS" \
    --header "x-api-key: $ANTHROPIC_API_KEY" \
    --header "anthropic-version: $ANTHROPIC_VERSION" \
    --header "anthropic-beta: $ANTHROPIC_BETA" \
    --header 'accept: text/event-stream' \
    "$url" >"$output" 2>&1 &
  SSE_PID=$!
}

stop_sse() {
  if [[ -n "$SSE_PID" ]]; then
    kill "$SSE_PID" >/dev/null 2>&1 || true
    wait "$SSE_PID" >/dev/null 2>&1 || true
    SSE_PID=""
  fi
}

wait_for_sse_file_id() {
  local output="$ARTIFACT_DIR/session-events.sse"
  local deadline=$((SECONDS + SSE_TIMEOUT_SECONDS))
  while ((SECONDS < deadline)); do
    if grep -Fq -- "$FILE_ID" "$output"; then
      stop_sse
      log 'session-events-sse output'
      sed 's/^/[sse] /' "$output"
      assert_not_contains "$output" '/mnt/session/uploads' 'SSE 不泄漏 worker 绝对路径'
      return
    fi
    sleep 0.2
  done
  stop_sse
  sed 's/^/[sse] /' "$output" || true
  die "SSE 在 ${SSE_TIMEOUT_SECONDS}s 内未收到 file_id=$FILE_ID"
}

cleanup_request() {
  local label="$1"
  local method="$2"
  local path="$3"
  log "cleanup-$label"
  echo '[curl command]'
  print_json_curl "$method" "$API_BASE_URL$path" ''
  curl --globoff --silent --show-error \
    --connect-timeout 3 --max-time 15 \
    --request "$method" \
    --header "x-api-key: $ANTHROPIC_API_KEY" \
    --header "anthropic-version: $ANTHROPIC_VERSION" \
    --header "anthropic-beta: $ANTHROPIC_BETA" \
    "$API_BASE_URL$path" 2>&1 | sed 's/^/[cleanup response] /' || true
}

cleanup() {
  local exit_status=$?
  trap - EXIT INT TERM
  stop_sse
  if [[ -n "$SERVER_LOG_PID" ]]; then
    kill "$SERVER_LOG_PID" >/dev/null 2>&1 || true
  fi
  if [[ "$CLEANUP" == '1' ]]; then
    if [[ -n "$SESSION_ID" ]]; then
      cleanup_request session DELETE "/v1/sessions/$SESSION_ID?beta=true"
    fi
    if [[ -n "$FILE_ID" ]]; then
      cleanup_request file DELETE "/v1/files/$FILE_ID?beta=true"
    fi
    if [[ "$CREATED_ENVIRONMENT" == '1' && -n "$ENVIRONMENT_ID" ]]; then
      cleanup_request environment DELETE "/v1/environments/$ENVIRONMENT_ID?beta=true"
    fi
    if [[ "$CREATED_AGENT" == '1' && -n "$AGENT_ID" ]]; then
      cleanup_request agent POST "/v1/agents/$AGENT_ID/archive?beta=true"
    fi
  fi
  printf '\n[artifacts] %s\n' "$ARTIFACT_DIR"
  exit "$exit_status"
}

wait_for_agent_answer() {
  if [[ "$WAIT_FOR_AGENT_SECONDS" == '0' ]]; then
    log 'WAIT_FOR_AGENT_SECONDS=0，跳过真实 worker 回答检查'
    return
  fi

  local deadline=$((SECONDS + WAIT_FOR_AGENT_SECONDS))
  local matched=0
  while ((SECONDS < deadline)); do
    request_json poll-agent-events GET \
      "/v1/sessions/$SESSION_ID/events?beta=true&order=asc&limit=1000&types%5B%5D=agent.message" '' 200
    if [[ -n "$EXPECTED_TEXT" ]]; then
      if jq -e --arg expected "$EXPECTED_TEXT" '
        any(.data[]?; .type == "agent.message" and (tostring | contains($expected)))
      ' "$RESPONSE_BODY" >/dev/null; then
        matched=1
      fi
    elif jq -e 'any(.data[]?; .type == "agent.message")' "$RESPONSE_BODY" >/dev/null; then
      matched=1
    fi
    if [[ "$matched" == '1' ]]; then
      echo '[PASS] worker 已读取文件并返回预期内容'
      assert_not_contains "$RESPONSE_BODY" '/mnt/session/uploads' '公开 agent event 不泄漏 worker 绝对路径'
      return
    fi

    request_json poll-session GET "/v1/sessions/$SESSION_ID?beta=true" '' 200
    local status
    status="$(jq -r '.status // "unknown"' "$RESPONSE_BODY")"
    echo "[poll] session status=${status}，继续等待 worker，剩余 $((deadline - SECONDS))s"
    sleep "$POLL_INTERVAL_SECONDS"
  done
  die "${WAIT_FOR_AGENT_SECONDS}s 内未收到包含预期内容的 agent.message；请结合 artifacts 和 SERVER_LOG_FILE 排查"
}

main() {
  if [[ "${1:-}" == '-h' || "${1:-}" == '--help' ]]; then
    usage
    return
  fi
  [[ $# -eq 0 ]] || die "未知参数：$*（使用 --help 查看用法）"

  require_command curl
  require_command jq
  require_command grep
  mkdir -p "$ARTIFACT_DIR"
  trap cleanup EXIT INT TERM
  start_server_log_tail

  log 'healthz'
  echo "[curl command] curl -sS '$API_BASE_URL/healthz'"
  curl --silent --show-error --fail --max-time 5 "$API_BASE_URL/healthz" | sed 's/^/[healthz] /' || die "本地服务不可用：$API_BASE_URL"

  local test_file="${TEST_FILE:-}"
  local mime_type="${MIME_TYPE:-text/plain}"
  if [[ -z "$test_file" ]]; then
    test_file="$ARTIFACT_DIR/e2e reference.txt"
    EXPECTED_TEXT="${EXPECTED_TEXT:-OMA_FILE_EVENT_MARKER_$RUN_ID}"
    printf 'This file is readable only through the mounted Session Resource.\nMarker: %s\n' "$EXPECTED_TEXT" >"$test_file"
  else
    [[ -f "$test_file" ]] || die "TEST_FILE 不存在：$test_file"
    EXPECTED_TEXT="${EXPECTED_TEXT:-}"
  fi
  local mount_path="${MOUNT_PATH:-/uploads/e2e reference $RUN_ID.txt}"
  local title="session-file-e2e-$RUN_ID"

  if [[ -z "$AGENT_ID" ]]; then
    local agent_body
    agent_body="$(jq -nc --arg model "$MODEL" --arg name "file-e2e-agent-$RUN_ID" '{model:$model,name:$name}')"
    request_json create-agent POST '/v1/agents?beta=true' "$agent_body" 200
    AGENT_ID="$(jq -er '.id' "$RESPONSE_BODY")"
    CREATED_AGENT=1
  else
    log "复用 Agent：$AGENT_ID"
  fi

  if [[ -z "$ENVIRONMENT_ID" ]]; then
    local environment_body
    environment_body="$(jq -nc --arg name "file-e2e-env-$RUN_ID" '{name:$name}')"
    request_json create-environment POST '/v1/environments?beta=true' "$environment_body" 200
    ENVIRONMENT_ID="$(jq -er '.id' "$RESPONSE_BODY")"
    CREATED_ENVIRONMENT=1
  else
    log "复用 Environment：$ENVIRONMENT_ID"
  fi

  upload_file "$test_file" "$mime_type"
  FILE_ID="$(jq -er '.id' "$RESPONSE_BODY")"

  local session_body
  session_body="$(jq -nc \
    --arg agent "$AGENT_ID" \
    --arg environment "$ENVIRONMENT_ID" \
    --arg title "$title" \
    '{agent:$agent,environment_id:$environment,title:$title}')"
  request_json create-session POST '/v1/sessions?beta=true' "$session_body" 200
  SESSION_ID="$(jq -er '.id' "$RESPONSE_BODY")"

  local event_body
  event_body="$(jq -nc \
    --arg file "$FILE_ID" \
    '{events:[{type:"user.message",content:[
      {type:"text",text:"Read the attached file and reply with the exact Marker value. Do not guess."},
      {type:"document",source:{type:"file",file_id:$file}}
    ]}]}')"

  if [[ "$RUN_NEGATIVE_CHECK" == '1' ]]; then
    request_json reject-before-mount POST "/v1/sessions/$SESSION_ID/events?beta=true" "$event_body" 400
    assert_contains "$RESPONSE_BODY" 'Session Resources API' '挂载前返回明确错误'
    request_json list-after-rejection GET "/v1/sessions/$SESSION_ID/events?beta=true&order=asc&limit=100" '' 200
    [[ "$(jq '.data | length' "$RESPONSE_BODY")" == '0' ]] || die '挂载前被拒绝的事件不应落库'
    echo '[PASS] 挂载前拒绝保持整批无副作用'
  fi

  local resource_body
  resource_body="$(jq -nc --arg file "$FILE_ID" --arg path "$mount_path" \
    '{type:"file",file_id:$file,mount_path:$path}')"
  request_json add-session-resource POST "/v1/sessions/$SESSION_ID/resources?beta=true" "$resource_body" 200
  RESOURCE_ID="$(jq -er '.id' "$RESPONSE_BODY")"

  start_sse
  sleep 0.5
  request_json send-file-event POST "/v1/sessions/$SESSION_ID/events?beta=true" "$event_body" 200
  assert_contains "$RESPONSE_BODY" "$FILE_ID" 'events.send 返回原始 file_id'
  assert_not_contains "$RESPONSE_BODY" '/mnt/session/uploads' 'events.send 不泄漏 worker 绝对路径'
  wait_for_sse_file_id

  request_json list-public-events GET "/v1/sessions/$SESSION_ID/events?beta=true&order=asc&limit=100" '' 200
  assert_contains "$RESPONSE_BODY" "$FILE_ID" '事件列表保留原始 file_id'
  assert_not_contains "$RESPONSE_BODY" '/mnt/session/uploads' '事件列表不泄漏 worker 绝对路径'

  request_json retrieve-session GET "/v1/sessions/$SESSION_ID?beta=true" '' 200
  [[ "$(jq -r '.title' "$RESPONSE_BODY")" == "$title" ]] || die 'Session 标题发生意外变化'
  echo '[PASS] Session 标题保持不变'

  wait_for_agent_answer

  log '端到端测试完成'
  echo '[PASS] Files upload → Session Resource → file_id event → SSE/public boundary → worker answer'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
