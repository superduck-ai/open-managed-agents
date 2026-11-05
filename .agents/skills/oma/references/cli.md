# ant 配合 OMA

## 安装与连接

```sh
brew install anthropics/tap/ant
ant --version
export OMA_BASE_URL='https://你的OMA域名'
printf 'OMA API key: '
IFS= read -r -s OMA_API_KEY
printf '\n'
export OMA_API_KEY
export ANTHROPIC_BASE_URL="$OMA_BASE_URL"
export ANTHROPIC_API_KEY="$OMA_API_KEY"
ant models list --format jsonl
```

以上输入命令在 Bash 中执行。完整优先级、URL 校验、凭据隔离与退出清理见 [连接配置](connection.md)。密钥由用户在本地输入。其他实例替换地址，base URL 不含 `/v1`。避免遗留 AWS/Google 路由配置影响目标；运行前核对实际配置，不输出认证值。已测试 ant 2.5.0，不保证任意新旧版本。

## Agent 文件与 apply

`agents/assistant.md`：

```markdown
---
name: OMA Assistant
model:
  id: deepseek/deepseek-flash
tools: []
---
按照用户要求完成任务，如实报告结果。
```

模型只是本次实例验证值，使用时先查列表。

```sh
ant apply --dry-run -v agents/assistant.md
ant apply --yes agents/assistant.md
ant apply --dry-run agents/assistant.md
```

`--yes` 用于已有执行授权的非交互调用。只指定本次文件，避免目录扫描误操作。保存生成的 `claude-lock.json`，丢失后可能重复创建。更换实例或 workspace 时使用独立目录和 lockfile。应用运行时把 ID 保存到应用配置，不依赖 CLI lockfile。

```sh
AGENT_ID=$(jq -r '.resources["./agents/assistant.md"].id' claude-lock.json)
ant beta:agents retrieve --agent-id "$AGENT_ID" --format json
```

## 环境：OMA 当前兼容差异

ant 2.5.0 的环境命令不发送 OMA 要求的 `beta=true` 查询参数；设置 `--beta managed-agents-2026-04-01` 仍失败。不要把 Environment 文件加入 ant apply。

```sh
curl --fail-with-body --silent --show-error \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  "$ANTHROPIC_BASE_URL/v1/environments?beta=true&limit=20"
```

有 `next_page` 时继续分页。创建一个无额外包的受限云环境：

```sh
curl --fail-with-body --silent --show-error \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H 'Content-Type: application/json' \
  "$ANTHROPIC_BASE_URL/v1/environments?beta=true" \
  --data '{"name":"OMA Assistant Environment","config":{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false}}}'
```

读取响应中的实际 `id`，赋值给 `ENV_ID`。受限示例需要单独验证具体业务访问；后续中文验收已在上述 limited 配置中完成两轮真实运行；不代表外部主机隔离效果已验证。

## 每次运行

```sh
SESSION_ID=$(ant beta:sessions create \
  --agent "$AGENT_ID" --environment-id "$ENV_ID" \
  --title 'OMA smoke test' --transform id -r)
ant beta:sessions:events send --session-id "$SESSION_ID" \
  --event '{type: user.message, content: [{type: text, text: "请回复 OMA_ONBOARD_OK"}]}' \
  --format json
ant beta:sessions retrieve --session-id "$SESSION_ID" --format json
ant beta:sessions:events list --session-id "$SESSION_ID" --format jsonl
```

`--format json` 的自动分页列表输出可能是多个 JSON 文档；机器消费列表使用 jsonl，不要当作单个 JSON 数组解析。投递响应的 `processed_at: null` 表示尚未确认处理。检查历史里实际 agent.message 和 `session.status_idle` 的 `stop_reason.type=end_turn`；错误、requires_action、budget_reached 分别处理。

自动等待设置例如 120 秒截止时间，每 2 秒读取一次；超时返回 Session ID 和当前状态供恢复，不重新创建 Session 或自动重发消息。原始 HTTP SSE 已收到事件；ant SSE 命令已尝试但未获得输出，暂不作为可靠完成信号：

```sh
ant beta:sessions:events stream --session-id "$SESSION_ID" --format jsonl
```

## 能力边界与来源

已验证：模型查询、Agent 创建、Agent 文件 apply、无改动重复 apply、修改 system 更新、Session 创建、消息投递、模型回复、事件历史与本轮 idle。Environment 通过上述 HTTP 接口验证。后续中文验收覆盖多轮、分页、409 冲突、版本快照、limited 环境运行和文件上传/删除；上传文件下载返回 HTTP 400。其余高级能力尚未验证，见 [中文验收记录](validation.md)。

流程参考 Claude 的需求 → Agent → Environment → Session → 接入，OMA 指令独立编写，字段以 OMA 实际行为为准：

- https://github.com/anthropics/skills/blob/main/skills/claude-api/shared/managed-agents-onboarding.md
- https://github.com/anthropics/skills/blob/main/skills/claude-api/shared/anthropic-cli.md
- https://github.com/anthropics/anthropic-cli

## 文件上传限制

`ant files upload --file ./input.txt --format json` 已通过。当前实例返回 downloadable=false，随后 `ant files download --file-id <文件ID> --output ./downloaded.txt` 返回 HTTP 400。不能承诺上传文件可下载；生成文件交付需单独验收。清理本次上传文件使用 `ant files delete --file-id <文件ID>`。
