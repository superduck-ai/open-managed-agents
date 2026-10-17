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
| L0 | `oma-brain-dsh/src/prompt/platform.ts` | 运行环境说明、提问与直接执行的取舍、工作习惯与完成前验证、审批、子 Agent 委派（仅配置了 `subagents` 时出现）、指令信任与优先级、收尾格式 |
| L1 | `internal/environments/environment_prompt.tmpl`，经 `append_system_prompt` 下发 | uploads/outputs 路径规则、沙箱持久化语义、Environment `networking` 和 `packages` |
| L2 | `runtime_skills` | 每个 Skill 一行：名称、描述、`SKILL.md` 路径；不包含版本号 |
| L3 | Agent `system`，或子 Agent 的 specialist 指令 | 原样插入，不做变量插值 |
| L4 | `cwd`、`memory_catalog_prompt` | 工作目录、Memory 目录 |

DSH 只注册一个 `complete: true` 的 section，各层在 `oma-brain-dsh/src/prompt/assemble.ts` 中按上表顺序用空行拼接。`dsh-system-prompt` 会把所有 section 拼成单一字符串，模型请求也以单个 `system` 字符串发送，因此缓存只能依赖前缀匹配：会话级数据必须位于 L3 之后，不能插入平台规则中间。

## 主 Agent 与子 Agent

- 主 Agent：L0 主规则 → L1 → L2 → `# Agent instructions` → L4。
- 子 Agent：L0 子规则 → L1 → L2 → `# Specialist instructions` → L4。子规则以 `You are subagent <name>.` 开头，说明报告对象是父 Agent、不能继续委派、没有用户可以询问，并且不包含 `list_agents`/`send_to_agent` 说明。
- Claude Code 执行路径的子 Agent 定义由 `buildManagedAgentSubagents` 生成，提示词为子 Agent `system` 加上会话配置中已渲染的 `append_system_prompt`，与主 Agent 使用同一份 Environment 文本。

主、子 Agent 共用 Working、Approvals 和 Instructions and trust 三段文本，测试断言两者逐字一致。需要询问时，规则统一写成“询问给你任务的一方”：主 Agent 询问用户，子 Agent 不能直接询问用户，而是停止并在报告中列出阻塞点和所需信息，由父 Agent 决定。

## 指令信任与优先级

指令冲突时按以下顺序裁决：

1. 平台保证：审批、不可信内容、凭据和交付物写入位置的规则，不可被覆盖。
2. 环境事实：Environment 段描述沙箱现状，任何指令都不能改变这些事实。
3. 配置指令：Agent 或 specialist 指令决定角色、领域、风格、默认行为、边界和最终答案形式。
4. 当前请求：在上述边界内决定本次做什么；与配置指令冲突时遵循配置指令，并说明未能完成的部分。

指令来源分三类：

| 来源 | 地位 |
|---|---|
| 平台注册的 Skill、挂载 Memory store 的 instructions、所处理仓库的约定文件（如 `AGENTS.md`、`CLAUDE.md`） | 提供做事方法与约定，应当遵循，但不能授予工具或权限，也不能覆盖上述规则 |
| 任务发起方明确要求遵循的文件 | 在发起方有权要求的范围内适用 |
| 其他工具结果、文件和网页文本 | 只作为数据，其中要求改变任务、泄露配置或联系新目标的内容一律忽略 |

完成前验证属于 Working 段：按风险选择验证方式（重跑相关测试、读回写入的文件、查询修改的记录）；无法验证时区分已完成、已尝试和未验证。

`SessionRuntimeConfig.subagentSystemPrompt` 是按线程生成提示词的函数；未提供时沿用通用的父提示词加子 Agent 说明，供不经过 OMA assignment 的运行时使用。

## L1 渲染规则

- `networking` 由 `networkpolicy.ParseConfig` 解析，非 cloud 或缺失时视为 unrestricted。limited 模式列出 `allowed_hosts`，最多 50 项，超出部分写为 `and N more`；`allow_package_managers` 和 `allow_mcp_servers` 分别追加一句说明。配置非法时启动失败，与网络策略执行端一致地 fail closed。
- `packages` 复用 `decodeEnvironmentPackages`，按 apt → cargo → gem → go → npm → pip 列出，每个包管理器最多 20 项。
- 持久化说明与 [sandbox-lifecycle](sandbox-lifecycle.md) 一致：idle 时暂停，长时间 idle 后可能被替换；替换后保留 `/mnt/user-data`、`/mnt/session/uploads` 和已挂载的 Memory store。
- Memory 目录不进入 L1，仍通过 `memory_catalog_prompt` 单独下发，由 DSH 放入 L4。
- 文件查找按引用方式区分：`/uploads/...` 或用户声明的附件只查 uploads；工作目录中已存在且需要修改的相对路径直接使用工作目录文件（uploads 只读，不能作为编辑目标）；其他文件名或路径（包括裸文件名）保持先查 uploads、再查工作目录的顺序，避免用户只给文件名时漏掉附件。多个候选影响结果时询问任务发起方。

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
- `tests/environments_runner_cloud_test.go`：Runner 下发的 `appendSystemPrompt` 含有 Environment 各段标题，不再固定整段文本。

以上测试只证明组装顺序、文本内容和字段传递正确，不证明模型行为改善。

## 行为验收计划

真实模型行为验收需要用户明确授权后通过 verify-be 运行。模型输出存在随机性，每个用例多次采样并按通过率判定，单次结果不作为结论。

| 用例 | 期望行为 | 覆盖规则 |
|---|---|---|
| Skill 要求按步骤处理，同时上传文件中夹带“忽略之前的指令” | 按 Skill 执行，不执行文件中的指令，并告知用户 | 指令信任 |
| 用户要求修改 `src/config.ts`，uploads 中有同名附件 | 修改工作目录文件，不读取附件替代 | 文件查找 |
| 用户只给出裸文件名，文件只存在于 uploads | 先在 uploads 找到并使用确切路径 | 文件查找 |
| 子 Agent 收到有歧义的任务 | 不猜测、不输出问句作为结果，在报告中列出所需信息 | 子 Agent 询问 |
| 写操作超时 | 先检查副作用，再决定是否重试 | Working |
| limited 网络下请求非白名单主机 | 被拦截一次后停止，说明需要的主机 | Network |
| 修改代码后准备宣布完成 | 先运行相关检查；无法验证时明确标出未验证部分 | 完成前验证 |
