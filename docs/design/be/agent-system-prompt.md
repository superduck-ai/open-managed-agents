# Agent 分层系统提示词

## 目标

托管 Agent 的系统提示词由平台规则、Environment 事实、Skill 目录、用户 Agent 指令和会话数据组成。各部分的变化频率不同，按稳定性从高到低排列，使跨会话可复用的内容位于前缀；同时让主 Agent 与子 Agent 各自拿到不自相矛盾的规则。

## 分层

```mermaid
flowchart TB
  L0["L0 平台规则<br/>DSH 静态文本，随 DSH 版本变化"] --> L1
  L1["L1 Environment<br/>Go 按 Environment 配置渲染"] --> L2
  L2["L2 Skills<br/>按 Agent 固定版本"] --> L3
  L3["L3 Agent 指令 / 子 Agent specialist 指令"] --> L4
  L4["L4 会话<br/>工作目录、Memory 目录"]
```

| 层 | 来源 | 内容 |
|---|---|---|
| L0 | `oma-brain-dsh/src/prompt/platform.ts` | 运行环境说明、提问与直接执行的取舍、工作习惯、审批、子 Agent 委派（仅配置了 `subagents` 时出现）、不可信内容、收尾格式、指令优先级 |
| L1 | `internal/environments/environment_prompt.tmpl`，经 `append_system_prompt` 下发 | uploads/outputs 路径规则、沙箱持久化语义、Environment `networking` 和 `packages` |
| L2 | `runtime_skills` | 每个 Skill 一行：名称、描述、`SKILL.md` 路径；不包含版本号 |
| L3 | Agent `system`，或子 Agent 的 specialist 指令 | 原样插入，不做变量插值 |
| L4 | `cwd`、`memory_catalog_prompt` | 工作目录、Memory 目录 |

DSH 只注册一个 `complete: true` 的 section，各层在 `oma-brain-dsh/src/prompt/assemble.ts` 中按上表顺序用空行拼接。`dsh-system-prompt` 会把所有 section 拼成单一字符串，模型请求也以单个 `system` 字符串发送，因此缓存只能依赖前缀匹配：会话级数据必须位于 L3 之后，不能插入平台规则中间。

## 主 Agent 与子 Agent

- 主 Agent：L0 主规则 → L1 → L2 → `# Agent instructions` → L4。
- 子 Agent：L0 子规则 → L1 → L2 → `# Specialist instructions` → L4。子规则以 `You are subagent <name>.` 开头，说明报告对象是父 Agent、不能继续委派、没有用户可以询问，并且不包含 `list_agents`/`send_to_agent` 说明。
- Claude Code 执行路径的子 Agent 定义由 `buildManagedAgentSubagents` 生成，提示词为子 Agent `system` 加上会话配置中已渲染的 `append_system_prompt`，与主 Agent 使用同一份 Environment 文本。

`SessionRuntimeConfig.subagentSystemPrompt` 是按线程生成提示词的函数；未提供时沿用通用的父提示词加子 Agent 说明，供不经过 OMA assignment 的运行时使用。

## L1 渲染规则

- `networking` 由 `networkpolicy.ParseConfig` 解析，非 cloud 或缺失时视为 unrestricted。limited 模式列出 `allowed_hosts`，最多 50 项，超出部分写为 `and N more`；`allow_package_managers` 和 `allow_mcp_servers` 分别追加一句说明。配置非法时启动失败，与网络策略执行端一致地 fail closed。
- `packages` 复用 `decodeEnvironmentPackages`，按 apt → cargo → gem → go → npm → pip 列出，每个包管理器最多 20 项。
- 持久化说明与 [sandbox-lifecycle](sandbox-lifecycle.md) 一致：idle 时暂停，长时间 idle 后可能被替换；替换后保留 `/mnt/user-data`、`/mnt/session/uploads` 和已挂载的 Memory store。
- Memory 目录不进入 L1，仍通过 `memory_catalog_prompt` 单独下发，由 DSH 放入 L4。

## 工具定义

工具的用法写在工具定义中，跨工具的策略写在 L0：

- 沙箱工具说明位于 `oma-brain-dsh/src/sandbox-tools/definitions.ts`，补充了 Bash 每次调用不继承 shell 状态、`on_timeout=background` 的读取方式、Read 的 0-based/exclusive 行号、Edit 的唯一匹配要求，以及 Grep/Glob 默认路径不包含 uploads。
- `send_to_agent` 的每个参数都有说明，并写明 `thread_id`、`agent_name`、`name` 三者只能选择一个。
- 线程上限统一使用 `MAX_LIVE_SUBAGENT_THREADS`，工具说明、L0 文本和运行时校验共用该常量。
- 工具说明不包含会话级数据，避免工具定义变化导致后续缓存失效。

## 未纳入本版本

- `<oma-context>`/`<oma-reminder>` 运行时注入：当前没有定时或 webhook 触发的会话，触发来源信号尚不存在。
- 区分用户拒绝、策略阻断和停止的工具错误前缀：当前拒绝统一返回 `Tool use denied by OMA`。
- 按子 Agent 自身配置过滤 Skill 目录：DSH 线程目前没有携带子 Agent 的 Skill 列表，子 Agent 使用会话的 Skill 目录。
- 在 transcript 中记录各层提示词的版本或哈希。

## 测试

- `oma-brain-dsh/tests/prompt.test.ts`：层顺序、无子 Agent 时不出现委派说明、会话数据变化不影响 L3 之前的前缀、子 Agent 提示词不含委派工具。
- `oma-brain-dsh/tests/worker-registration.test.ts`：`append_system_prompt` 与 `memory_catalog_prompt` 分别映射到 assignment。
- `internal/environments/environment_prompt_test.go`：非法配置、默认 unrestricted、limited 网络与 packages 渲染、无白名单主机、长列表截断。
- `internal/environments/subagents_test.go`：Claude Code 子 Agent 定义带有渲染后的 Environment 提示词。
