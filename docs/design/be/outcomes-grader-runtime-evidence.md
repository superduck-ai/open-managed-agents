# Outcome grader：Claude 原生上下文执行入口的代码证据

> 2026-09-16；静态机制研究，未执行 Claude、模型调用或测试；不是已批准的选型。当前讨论边界见 [运行时复用复评](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md)。
> 本文是 **Anthropic 发布产物的实现证据**，不是 Managed Agents 产品合同，也不证明 Managed Agents 内部使用同一实现。hidden workflow 仅作为机制记录，不是 grader 优先方案或用户必须先接受的版本耦合决策。当前明确讨论的同 sandbox A/G 双进程候选不以 workflow 为前提，未最终选定或实现。外部文档仍严格限于 `/docs/en/managed-agents/`，未使用普通 Claude Code/Agent SDK 文档。

## 1. 来源、版本与可复核性

本轮取得 npm `latest` 为 **2.1.273**。latest 是查询时事实；落地应固定本次验收过的最新版本及摘要，不让未验收的未来版本自动改变协议。

- 发布包：`@anthropic-ai/claude-code@2.1.273`、`@anthropic-ai/claude-code-linux-x64@2.1.273`。
- [主包元数据](https://registry.npmjs.org/@anthropic-ai%2fclaude-code/2.1.273)、[Linux 包元数据](https://registry.npmjs.org/@anthropic-ai%2fclaude-code-linux-x64/2.1.273)。它们是发布产物来源，不作为 Managed Agents 文档引用。
- 两个 tgz 均已核对 npm `dist.integrity` 的 SHA-512 SRI；这是完整性核对，**不是另行验证 npm 发布签名**。
- Linux ELF SHA-256：`6c752e2cc7c110c9df15f26d8d134d438c5ae95dbd610efc1a308bf7f9c5f6c1`。
- 原始包、元数据、二进制和提取索引位于 [本轮证据目录](/tmp/oma-runtime-static-20260916)。没有安装、执行或修改该二进制。
- 从 ELF 提取以 `// @bun @bytecode\n// Claude Code` 开始、NUL 结束的内嵌 JavaScript，共 1775 段。不是 TypeScript 原仓库，也不是仅从字符串存在性猜测能力：下文追踪调用、参数和分支。
- `embedded/` 保存原文，`pretty/` 是用当前仓库 Prettier 格式化的阅读副本。行号仅定位阅读副本；**版本、ELF 摘要、字节区间及原始段摘要**定义证据。

| 标识 | ELF 字节区间，左闭右开 | 原始段 SHA-256 | 内容 |
| --- | --- | --- | --- |
| C | 193420812–199305790 | `7358747b3884d1e3c094862fa61d6e7ef4d50541c5b45a918d284735ff4afddd` | agent 配置、`DE` 子执行器、skills、hooks |
| S | 206736349–206776407 | `66b10886fcb0455aee332898cd5a507b25043c4be6abe812dcb189e32568350f` | slash command 分派、forked skill |
| P | 212767901–213204439 | `68376c243fbba54930a57ad4d5ca6d47eb61cecd08d59cfa020f20fc04024957` | headless 输入、initialize、result |
| Q | 202915451–203417707 | `92f69e58c4da64c7a9d42a02728f94af6d2570082ed8a922aa9490f7b827c85c` | 用户输入处理、原生 query/stop 循环 |
| T | 212533200–212573639 | `35733d39c0d3cb7473d09dcb9eab2d963f263c525f42d057744bb000756d6839` | 双向控制 IO、hook callback |
| G | 192714160–192715111 | `cc8bff5571383bcc4b693cdd210160182cf74550b86dce90ef457bd868bae877` | coordinator-mode 分支 |
| WA | 205391767–205480209 | `fee988e43150d4bcc4f0fc0f97ab18c0828e39a95c97a407811a2553217c83e2` | workflow agent 执行、schema、子上下文元数据 |
| WE | 205486298–205497347 | `10cf1856a80d75608a1d75211648b998a6ab83d49dbc362e1e0b5eb1fe0ac872` | environment-delivered workflow 命令 |
| WX | 205481440–205486297 | `08d18c769fc5a0a3e0a72953fd0ed9da3eeb2eeacc657cdb15265cbb5cf6d1a6` | 共用 workflow 执行入口与结果 |
| WP | 191538239–191541426 | `67fe934bec6a40263ab8bcc20989df4da70fbe86e58deac5a287f0cde34619d3` | workflow 配置/组织策略门控 |
| W | 205497348–205506984 | `9b1e508de57ef4783294174bc01dd519be3f7c88458254f079d5684fdb4e3320` | server-dispatched workflow launch |

后续复核可按元数据下载同版本 tgz、校验 SRI、取出 `package/claude`，按上表切片并核对摘要。不需要运行二进制。不要把约 218 MiB 的发布物加入仓库。

## 2. R1：原生子上下文，不是第二个模型 loop

[C:82111–82183](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L82111) 的 `Oxo/jot` 将 agent 定义解析为独立 `model/getSystemPrompt/tools/disallowedTools/mcpServers/hooks/maxTurns/skills/omitClaudeMd` 等运行配置。

[C:184112–184223](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L184112) 的 `DE`：

- 创建/接收独立 `agentId`、abort controller，选择该 agent 的 model。
- `forkContextMessages` 只有显式传入才参与初始 history；缺省时历史为本次 `promptMessages`。
- 创建独立 read-file state；不是把 primary ID 改个名字继续使用 primary history。
- [C:184265–184407](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L184265) 选择 agent system prompt、过滤 tools、连接该 agent 的 MCP。
- [C:184840–184868](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L184840) 最终调用已有 `rl.options.runQuery`，传入子 context 的 messages/system/tools/model/maxTurns。模型采样和 tool loop 已在运行时内。

**可得结论**：同一个 Claude 进程具有原生多上下文执行能力；不能从 Rust EM 只创建一个 executor 推导“一 sandbox 只能一个 Claude context”。同时，R 当前生产装配尚无一 session 管理多个独立 Claude 进程的能力；这不是 OS/sandbox 限制，具体共享身份与生命周期边界见[主复评 3.2](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md)。

**限制**：子工具集首先来自 available pool；MCP 会合并父 clients 与 agent clients，再按配置过滤，不能把“配置里写了 tools/MCP”视为已经完全隔离。`omitClaudeMd` 也不是无条件的安全隔离开关：`cbs` 在 managed instructions 获取失败时会回退原 userContext（C:185228–185236）。还须处理 session hooks、skills、append-subagent prompt 与共享文件输入。

## 3. R2：不依赖 writer 自愿委派的确定性入口已找到

完整静态调用链：

```text
内部调度消息中的显式 /skill-name 参数
  → headless user-input queue
  → processUserInput / processSlashCommand
  → context: fork 分支
  → M0t 选择 skill 指定的 agent，构造新 promptMessages
  → dt 调用 DE
  → Claude 原生 query/tool loop
```

具体证据：

1. [P:20804–20854](/tmp/oma-runtime-static-20260916/pretty/chunk-212767901.js#L20804) 的普通 user 输入进入 primary queue；peer/client-composed 等分支会设置 `skipSlashCommands`，不能混用它们。
2. [P:9100–9136](/tmp/oma-runtime-static-20260916/pretty/chunk-212767901.js#L9100) 将输入和 skip 标记传入 `ZUe`；[Q:3858–3900](/tmp/oma-runtime-static-20260916/pretty/chunk-202915451.js#L3858) 明确调用 slash dispatcher，并传入 hook executors。非交互运行本身没有取消这条分支。
3. [S:1600–1690](/tmp/oma-runtime-static-20260916/pretty/chunk-206736349.js#L1600) 对 `sQt(...) === "fork" && !bMe(context)` 直接调用 `dt`，不是让 writer 先作一次模型决策再调用 Agent tool。
4. [C:170860–170987](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L170860) 的 `M0t` 按 skill 的 `agent` 选择 agent 定义；消息由 skill 正文和显式附件构造。**若 agent 不存在会回退 general-purpose/其他 agent**，因此 OMA 必须在启动确认中校验实际定义，不能允许静默回退成为评分依据。
5. [S:566–593](/tmp/oma-runtime-static-20260916/pretty/chunk-206736349.js#L566) 的 `DE` 调用没有传入 `forkContextMessages`。因此这条调用链不是复制 writer history 的 conversation fork。

**工具池的硬边界**：这条 skill 路径的 `availableTools` 来自 parent `r.options.tools`（C:170972–170986），不同于普通 Agent 路径会用 `ZN` 重建候选工具池（C:191187–191194）。skill 的 allowed-tools 不是任意补回被移除 builtin 的机制。若完整配置允许 writer 无 Read/Bash 而 grader 需要它们，不能只套 skill 就宣称支持；也不能把 grader 工具无条件暴露给 writer 来绕过。必须证明所选控制/config 路径保留了各自工具面，或改选独立原生执行承载。这个限制已是源码结论，不是一次成功样例可以消除的问题。

**关键条件**：G 的 `bMe` 表示 primary 处于 `CLAUDE_CODE_COORDINATOR_MODE` 时阻止该 fork 分支；它不等于普通“业务上有 coordinator”。OMA 未显式设置该变量，不代表实际容器环境一定没有它。应明确管理有效环境与输入标记，不通过修改上游 bundle 绕过门控。

**结论等级**：已证实发布实现存在确定性入口；未宣称 H/R 现有部署已配置并运行成功。私有 skill 必须由受信控制面发起，公共用户文本不能冒充内部评分指令；writer 也不应获得自由启动私有 grader 的授权。Agent 原生 deny/allowedAgentTypes 在 C:190698–190730 有实际执行检查，不仅是 prompt 隐藏。

## 4. R3：原生 Stop agent hook 不是完整 outcome 引擎

[C:258240–258460](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L258240) 的 `mOr` 确实启动独立 agent query，可检查真实文件并返回 `ok/reason`。这进一步支持复用原生能力，而不是另写推理 loop。

但是直接拿它当完整 grader 有明确差异：

- 系统提示词主动提供父会话 transcript 路径供其阅读，不满足默认“不受 writer 实现过程影响”的输入选择目标。
- 工具来自父工具集过滤；不是随意配置一套完整独立 grader runtime。
- 缺省 60 秒、50 个 assistant 消息上限；拿不到有效 structured output 时返回 cancelled。
- 核心执行器只消费 `ok/reason`，没有 Managed Agents 的 outcome/iteration/start-id/usage/terminal 合同。
- [Q:12592–12611](/tmp/oma-runtime-static-20260916/pretty/chunk-202915451.js#L12592) 默认连续 Stop blocking 上限是 8；超过后强制结束 turn。它不是官方 `max_iterations`（最大 20）的同义状态机。

**取舍**：不把 agent-type Stop hook 直接包装成完整功能。可复用 **Stop/SubagentStop 生命周期回调** 观察与校验，但由 OMA 持有 outcome 状态，Claude 持有模型/tool loop；二者不是另造一套模型循环。

## 5. R4：结果与 usage 必须按上下文接收，不能信外层 success

### 5.1 原生 callback 是存在的，当前 OMA 尚未消费

[P:21306–21309](/tmp/oma-runtime-static-20260916/pretty/chunk-212767901.js#L21306) 注册 initialize 的 hooks；T 的 `createHookCallback` 发出：`subtype: hook_callback, callback_id, input, tool_use_id`，等待控制回复。其非取消异常路径会记录错误并返回空结果，**不可把 hook 本身当成 fail-closed 的唯一门闩**。

[C:259644–259698](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L259644) 的 `SubagentStop` 输入包含 `agent_id/agent_type/agent_transcript_path/last_assistant_message`；也包含后台任务信息。它是停止前回调，不是“结果已经可靠提交且费用结算完成”。

OMA [service.go:336–347](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/service.go#L336) 当前只处理 `can_use_tool`，其他 control_request 为 no-op。所以只加 hooks 初始化会等待没有接线的回调；必须补充私有消费、合法回包和去重。

### 5.2 JSON schema 没有自动传给该子上下文

initialize 的 `jsonSchema` 是顶层入口（P:21309）；`DE` 虽有 `requiresStructuredOutput` 参数（C:184157），**上述 slash 调用没有传入它**。不能从 CLI 支持 `--json-schema` 推导每个子 agent 都已有独立强类型输出。

**只针对 forked-skill 候选**，补足设计是：私有结果合同由 OMA 校验；可用 SubagentStop 回调采集并验证 JSON candidate，必要时让原生 loop 修正格式。有效 candidate、子执行结束、取消/epoch 状态满足后才提交评价。缺有效结果不得成功，格式修复次数也不是 outcome 修订轮次。不先新增一套 MCP 服务或模型循环；若回调无法覆盖后续验收，再比较专用 schema tool。

### 5.3 外层 result 的陷阱是代码可证的

- S:608–638 的 fork 异常路径可以返回 `shouldQuery: false` 和错误消息；S:642–657 的正常路径也可 `shouldQuery: false`。
- [P:9230–9264](/tmp/oma-runtime-static-20260916/pretty/chunk-212767901.js#L9230) 对不再查询 primary 的路径统一发 `subtype: success`，`is_error: false`、`num_turns: 0`、`stop_reason: null`，并带 `resultText`。这不足以证明 child 成功，更不证明 rubric 满足。
- 该路径 `usage` 来自本回合初始化的累计器（P:8934），子查询不经过 primary stream 的 usage 累加分支；`modelUsage: Ak()` / `total_cost_usd: Tu()` 又是进程/会话级聚合入口。不能把外层结果原样当单轮 grader usage。
- OMA [mapper.go:386–447](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/mapper.go#L386) 当前又把所有 result 投影成 session idle，并可能从 wall duration 合成模型请求 span。必须在这个信息压缩之前识别私有控制回合。

### 5.4 有 context 元数据机制，但不可无条件套用

[C:116658–116690](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L116658) 的 API client 能加入 `x-claude-code-agent-id` 和 parent ID；main agent 不加入。普通 Agent 路径有 agent context，这不是必须新增模型代理的理由。

但同步 slash 的 `DE.override` 未显式传 `agentContext`；`X$n` 默认继承调用方 context（C:171036），因此**不能直接保证这条路径每个模型请求都带独立 child header**。per-context transcript/lifecycle ID 与原始 usage 才是需要对齐的证据；预算的 session 总账与 grader 分账分开处理。请求头是关联信息，不是可覆盖 tenant/权限/可信 agent 身份的凭据。

## 6. R5：其他原生入口的能力与限制

| 入口 | 实际证据 | 结论 |
| --- | --- | --- |
| 普通 Agent + SendMessage | 原生多上下文、持久任务、独立定义 | 适合 roster；若仅提示 writer 自行启动 grader，无法保证自动评分 |
| `side_question` | P:19686–19742 与 `chunk-212745109.js:22–78` 使用 writer 的缓存上下文/history | 不是独立 grader 默认输入 |
| `fork_conversation` | P:12261 起复制当前 conversation，另建远端历史 | 不是空白 grader 上下文；也不等于 forked skill |
| `workflow_launch` | W:23–98 校验 `/.workflow/` artifact/摘要/长度；W:295–379 每 session 最多一个不同 launch | 不是每轮 outcome 都可调用的通用 spawn；重新设计整个 session 为长驻 workflow 不是已证明更小的方案 |
| 给 user 消息加 `session_thread_id` | P:20804 起普通消息仍进入 primary queue；未找到该字段的输入处理 | 不能靠多加一个 JSON 字段实现 child 路由；官方也没有要求客户端直接向 child 发 user.message |
| 原样发送 `user.define_outcome` | worker transport/输入分派没有对应执行处理证据 | OMA 应消费业务事件并翻译成已核实的原生动作，不能假定运行时内置 Managed Agents outcome engine |

## 7. R6：实现机制记录——单步 workflow 的 agent({schema})

**继续追踪工具池限制后找到，不应再把所有 workflow 入口等同一次性 workflow_launch。** 本节只记录该机制相对 forked skill 的能力差异；它仍调用同一个 `DE` 和原生模型/tool loop，不据此推荐 grader 必须或优先走 workflow。

### 7.1 确定性、可重复的入口

- [C:252946–252957](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L252946) 注册 `__remote-workflow`：`type: local`、`isHidden: true`、`disableModelInvocation: true`、`supportsNonInteractive: true`；C:253517 将它加入命令表。这不是猜测出来的未注册命令。
- [WE:257–295](/tmp/oma-runtime-static-20260916/pretty/chunk-205486298.js#L257) 从 `CLAUDE_REMOTE_WORKFLOW_SCRIPT/ARGS` 读取已配置脚本/参数，检查 `CLAUDE_CODE_REMOTE` 与 workflow policy，直接进入 `nkt`。它不让 writer 模型决定是否调用 workflow。
- [WX:54–117](/tmp/oma-runtime-static-20260916/pretty/chunk-205481440.js#L54) 每次调用产生新的 workflow run ID 并调用 `Ket`；**没有 W 的 one-distinct-launch-per-session 分支**。脚本仍是启动环境中的固定内容，不能把 slash 后的参数误当已支持动态替换脚本/args。
- OMA [environment_manager.go:270](/Users/yueqi/Coding/Agent/open-managed-agents/internal/environments/environment_manager.go#L270) 已设置 remote；Rust [build_child_environment:722–790](/Users/yueqi/Coding/Agent/environment-manager-rs/src/internal/claude/claude_code_executor.rs#L722) 有 startup env 传入路径。脚本变量必须平台保留，不允许公共请求覆盖。

这条入口没有经过 forked-skill 的 `!bMe` 分支，因此不能把 skill 的 coordinator-mode 限制照搬过来；但仍受命令可用性、remote 和真实组织策略约束。

### 7.2 相对直接 skill 的工具池、schema 与身份差异

[WA:2243–2378](/tmp/oma-runtime-static-20260916/pretty/chunk-205391767.js#L2243) 选择 `agentType` 对应的定义；不存在或被策略拒绝会报错，**不是静默回退到 general-purpose**。使用 `ZN` 重建候选工具池，再由 grader 定义过滤，不只是对 writer 当前 `options.tools` 取子集。仍遵守全局安全策略，不是绕过 deny；相异 MCP 还须明确过滤继承的 client/tools。

`schema` 进入实际执行路径，而非只放入提示词：

1. WA:2308–2336 校验 JSON Schema（包括不可满足 schema），创建 StructuredOutput tool。
2. WA:2575–2581 把该 schema tool 放进子上下文可用工具。
3. [WA:2620–2640](/tmp/oma-runtime-static-20260916/pretty/chunk-205391767.js#L2620) 创建明确的 child agent context，含 agentId、parent、workflowRunId。
4. [WA:2731–2764](/tmp/oma-runtime-static-20260916/pretty/chunk-205391767.js#L2731) 调用 `DE`，设置 `requiresStructuredOutput` 和 `override.agentContext`，没有传 `forkContextMessages`。输入可以有原生来源授权说明，不等于继承 writer history；仍须控制 hooks/CLAUDE.md 的额外注入。
5. WA:2766–2805 消费 structured_output，检查失败次数并由原生执行机制处理 schema 错误。不是 OMA 自己调用模型修复 JSON。

因此同步 skill 的三个具体问题——候选 builtin 工具池、子请求身份、独立 structured output——在此有更直接的原生实现支撑。C 的 API client 会使用明确 child context 设置 agent-ID 关联 header；OMA 仍需验证所属 session/attempt，不能让 header 改写授权身份。

### 7.3 若采用此机制的接入设想，不是当前选型或已接线事实

以下仅适用于将来选择该 workflow 机制的情况：脚本只做“一次 grader agent 调用并返回结构化结果”，不在脚本里重复建设 outcome 状态机；每次业务评价由 OMA 发起一个 run。模型/tool/格式修复循环全部属于 Claude，不将这些脚本/callback 要求套到双进程候选。

固定脚本不要求固定 rubric：已有 [SubagentStart](/tmp/oma-runtime-static-20260916/pretty/chunk-193420812.js#L259521) callback 带 agent_id/type；`DE` 将回调的 additionalContexts 加入初始消息（C:184338–184353）。OMA 可按受信派发/active attempt 提供本次 description、rubric、artifact 范围与 attempt ID，先完成身份/输入登记，再允许评分请求。这个动态输入桥需要实现；当前 OMA 忽略 callback，不能把它写成现有能力。

[WX:120–186](/tmp/oma-runtime-static-20260916/pretty/chunk-205481440.js#L120) 返回 `remote-workflow:` 后的 envelope，包括 status/runId/result/failures；failed/killed 走错误路径。输出有大小上限，可能出现 result omitted。OMA 必须解析 envelope、校验评分 schema/attempt、拒绝缺失或截断结果；**外层本地命令 success 仍不是权威评分结果**。

内部 child transcript、结构化 result、request 计量用于 grader 私有归集；公开只发 outcome spans/explanation/usage。原生 workflow journal 有恢复设施，但不能据此宣称 OMA attempt/epoch/外部工具副作用已经 exactly-once。

### 7.4 该机制的版本/策略风险记录

- `__remote-workflow` 是 **hidden、environment-delivered 的发布实现接口**，不是 Managed Agents 官方公开稳定 API。若采用，须固定验收版本并在升级时核查接口，不修补 bundle 或伪造 policy 放行；不要求用户在本轮先接受这项耦合。
- WX:46–52 明确检查 disableWorkflows 与 allow_workflows；环境脚本入口没有 server-authored carrier 的特殊放行参数。若实际部署策略不允许，判为该环境不可用，不借本研究绕过。
- 普通 roster、grader 配置与会话安全策略要分层；不能通过给 writer 额外工具来满足 grader。原生独立工具池不代替 OMA 自己的 context-aware 权限判定。
- 这是静态机制证据，不是已运行通过或已批准的复用路线。取消/恢复/预算/隐私合同与 outcome 控制状态、触发反馈不因采用或不采用 workflow 而消失。

## 8. 本文已经确定和没有宣称的内容

已经确定：原生 1:N 上下文引擎、两类确定性分派入口、单步 workflow 的独立配置/structured output/context 身份链、外层 result/usage 的陷阱、OMA 的具体接线缺口。

尚不能宣称：Anthropic Managed Agents 就用本文 skill/workflow 或双进程路径；双进程天然隔离历史/权限；R 已有一 session 多独立 Claude 进程管理；部署环境门控已满足；每种 tool/MCP/取消/恢复/计费组合已运行通过。后者是实现后的验收，不以现在提前跑一两个样例代替完整设计。
