# Outcomes Grader 位置与前置条件调研

> Issue: #246 ｜ 关联: #11（grader 架构决策）
> 日期: 2026-09-16（含同日独立审计结果）
> 分支: `fix/outcomes-link-and-grader`
> 状态: **未决**。原有推荐方案已被审计证伪，方案空间重新开放（见 §3、§5）

本文固化「outcome grader 应该跑在哪里」这一决策所需的证据，避免重复调研与失忆。

---

## 证据分级约定

本文所有陈述按下列四级标注，混级视为错误：

| 标记 | 含义 |
|---|---|
| **【原文】** | 官方文档逐字引用，附文件:行号。已经独立审计复核 |
| **【代码】** | 本仓库或 environment-manager 仓库的代码事实，附文件:行号。已经独立审计复核 |
| **【推断】** | 由上述事实推出的判断。**必须写明承重点与失效条件** |
| **【假设】** | 未经验证的判断。不得作为决策依据 |

> **本文档的历史教训**：初版把若干【推断】与【假设】写成了【原文】的口吻，
> 导致基于错误前提否决了候选方案。§9 记录了完整审计结果。

---

## 0. 来源说明（重要）

本仓库有两份来源不同、**不可混用**的官方文档：

| 来源 | 位置 | 性质 | 引用时 |
|---|---|---|---|
| **官方原始镜像** | `docs/api-gap` 分支 → `docs/managed-agents-reference/` | 官方站点原始 Markdown，**未经改写**。写入 "Anthropic-managed sandboxes" | ✅ 引用官方原文以它为准 |
| OMA 改写版 | 本分支 → `docs/en/`、`docs/zh/` | 由官方文档改写而来，把 "Anthropic" 替换为 "OMA" | ⚠️ 仅作对照 |

官方页面 URL 映射见 `docs/managed-agents-reference/README.md`（27 页清单，下载日期 2026-08-22）。

**【原文】** 两份文档存在**内容差异**，不只是措辞：`docs/managed-agents-reference/tools.md:642` 有一句
grader 工具集契约，而 OMA 改写版 `docs/en/tools.mdx` 中 `grader` 命中次数为 **0**——
整个 "Multiagent sessions, outcomes, and mid-session updates" 小节都未进入改写版（已审计复核）。

---

## 1. 官方原文摘录

### 1.1 grader 是什么

**【原文】** `docs/managed-agents-reference/define-outcomes.md:9`
官方页面: https://platform.claude.com/docs/en/managed-agents/define-outcomes

> When you define an outcome, the harness automatically provisions a *grader* to evaluate the artifact against a rubric. The grader uses a separate context window to avoid being influenced by the main agent's implementation choices.

**【原文】** 同文档 `:11`

> The grader returns an explanation summarizing which criteria passed or failed, or confirming that the artifact satisfies the rubric. That feedback is handed back to the agent for the next iteration.

**可提取的硬约束（仅这四条，不要再加码）**：
1. grader 由 **harness** provision（不是 agent 自己发起）
2. 评估对象写作 **"the artifact"**——单数，**官方未定义其物理位置**
3. grader 使用**独立的 context window**
4. grader 产出 explanation，并**回灌给 agent**进入下一轮迭代

**【原文】** `:713`（全文仅此一处提到产物路径）

> The agent writes output files to `/mnt/session/outputs/` inside the sandbox.

⚠️ 此句**只写 "inside the sandbox"，未限定 Anthropic-managed**。
把产物路径与 Anthropic-managed 绑定的表述在 `self-hosted-sandboxes.md:48`（见 §1.3）。

### 1.2 grader 的工具集

**【原文】** `docs/managed-agents-reference/tools.md:642`

> The grader in outcome-driven sessions runs without `web_search` and `web_fetch`, regardless of these settings.

**⚠️ 语境更正（初版曾误读）**：此句位于 **web 域名过滤小节**
（"Multiagent sessions, outcomes, and mid-session updates"），不在工具架构章节。
"regardless of these settings" 的先行词是 `allowed_domains` / `blocked_domains`。

**【原文】** 同页另有说明：`web_search` 与 `web_fetch`
**run on Anthropic's servers whether the environment is a cloud or self-hosted sandbox**。

**因此本句能提取的只有两条**：
- grader **会**使用工具
- grader 没有 `web_search` / `web_fetch`

**不能**从中推出「grader 工具集是 session 工具集的子集」、「grader 与 writer 共用同一套工具实现」——
减掉的是两个**服务端**工具，对沙箱工具执行面零信息。详见 §9 的 D2 裁定。

### 1.3 交付物位置（两种环境类型不同）

**【原文】** `docs/managed-agents-reference/self-hosted-sandboxes.md:48`（Sandbox filesystem 节）

> **Outputs:** on self-hosted environments the session's system prompt omits the `/mnt/session/outputs` instruction used on Anthropic-managed sandboxes, so final deliverables land wherever the agent writes them in your sandbox filesystem, typically under the working directory.

**⚠️ 语义边界（初版曾过度解读）**：本句的含义是「self-hosted 下**不强制**写到 `/mnt/session/outputs`」，
**不是**「产物不可枚举、不可定位」。默认工作目录仍是 `/workspace`，
"wherever" 指「不限定路径」，而非「无法发现」。

### 1.4 多 agent 共享沙箱

**【原文】** `docs/managed-agents-reference/multiagent-orchestration.md:17`

> All agents share the same sandbox, filesystem, and vault credentials, but each agent runs in its own **session thread**, a context-isolated event stream with its own conversation history.

**【原文】** 同文档 `:21`

> Each agent uses its own configuration: model, system prompt, tools, MCP servers, and skills. Session-level agent configuration overrides are the exception; they apply to the coordinator and its `self` copies. Tools, MCP servers, and context are not shared.

**⚠️ 范围警告**：本节通篇描述 multiagent orchestration，**全文未提及 grader**。
不能据此推断 grader 也在同一沙箱。本节的用途仅是证明
「官方基础设施支持『一沙箱 + N 个上下文隔离执行』这一形态」。

**⚠️ 反向提示**：`:21` 明确「Tools, MCP servers, and context are **not shared**」——
它同时是「同沙箱 ≠ 共用工具实现」的官方例证。

### 1.5 沙箱在 idle 时被 checkpoint

**【原文】** `docs/managed-agents-reference/events-and-streaming.md:2379`

> When a session goes idle, its sandbox is checkpointed, preserving the full sandbox state, including the filesystem, installed packages, and any files the agent created.

**【原文】** 同页 `:2382`：沙箱状态自创建起保留 **30 天**，活动不延长该窗口。

### 1.6 事件与结果语义

**【原文】** `docs/managed-agents-reference/define-outcomes.md:565-627`、`webhooks.md:27`
**【代码】** `openapi/oma.en.json`

| 事件 | required 字段 |
|---|---|
| `span.outcome_evaluation_start` | `type, id, processed_at, iteration, outcome_id` |
| `span.outcome_evaluation_ongoing` | `type, id, processed_at, iteration, outcome_id` |
| `span.outcome_evaluation_end` | `type, id, processed_at, outcome_evaluation_start_id, iteration, result, explanation, usage, outcome_id` |

**【原文】** `span.outcome_evaluation_end.result` 取值：
`satisfied` / `needs_revision` / `max_iterations_reached` / `failed` / `interrupted`

**【原文】** `usage` 定义：**"Aggregate token usage for this evaluation cycle. Sums across all grader model requests within the cycle."**

**⚡ 两套 result 不要混淆**（实现时最易踩的坑）：

| | 取值 |
|---|---|
| **span 事件**的 `result` | `satisfied` / `needs_revision` / `max_iterations_reached` / `failed` / `interrupted` |
| **`outcome_evaluations[].result`** | `pending` / `running` / `evaluating` + 终态 `satisfied` / `max_iterations_reached` / `failed` / `interrupted` |

`needs_revision` **只存在于 span 事件**，不是资源取值——该轮结束后资源回到 `running`。

**【代码】** `BetaManagedAgentsOutcomeEvaluationResource` required 字段：
`type, outcome_id, description, result, iteration, completed_at, explanation`

**【原文】** `webhooks.md:27`：

> `session.outcome_evaluation_ended` | Outcome evaluation for **a single iteration** completed.

---

## 2. 官方文档**没有**说什么（已审计复核）

以下四点经全量检索确认**不存在**（审计独立复现，见 §9 A6/A7）：

1. ❌ **没有**规定 grader 的**物理执行位置**（同沙箱 / 独立沙箱）
2. ❌ **没有**说明 grader 能否读 session 沙箱的文件系统
3. ❌ **没有**说明 grader 是否是一个 thread
4. ❌ **没有**声明 outcomes 支持或**不支持** self-hosted 环境

> **⚠️ 解读纪律**：第 4 条是**否定命题**，只能说明「没写」，**不能**推出「两种环境都支持」。
> 同文档对真正不支持的能力是**写死 400** 的（如 self-hosted 不支持 `resources`）。
> 「未声明限制」是弱证据。初版在此处犯了沉默论证的错误（§9 D1）。

---

## 3. ~~推断：grader 在 session 自己的沙箱内~~ —— **已证伪，勿再使用**

初版给出过一条"grader 与 writer 同沙箱"的推理链，**已被独立审计击穿**。完整记录：

### 3.1 初版推理链（保留供追溯）

```
P1  官方要求 grader「evaluate the artifact」
P2  self-hosted 交付物「land wherever the agent writes them」
P3  outcomes 特性对两种环境类型都可用
P4  跑在另一个文件系统里的 grader 无法定位产物
 ⇒  grader 在 session 自己的沙箱内
```

### 3.2 被击穿的三处

| 步骤 | 问题 |
|---|---|
| **P2** | **过度解读**。原文意为「不强制写 `/mnt/session/outputs`」，**不是**「不可枚举」。默认工作目录仍是 `/workspace` |
| **P3** | **沉默论证**。同文档对不支持的能力会写死 400；「没提限制」≠「两种环境都支持」 |
| **P4** | **被官方原语打破**。§1.5 的 idle checkpoint 是**整盘快照**（含文件系统），把快照挂到另一沙箱即可看到那些文件，**不必同沙箱** |

### 3.3 因此

- 「grader 需要能读到产物」这一**弱命题**仍站得住
- 「**所以 grader 运行在 session 自己的沙箱内**」是**额外的实现选择，不是必然推论**
- 初版用 P2/P4「排除独立沙箱」的做法**失败**

---

## 4. OMA 现状与缺口（【代码】，已审计复核）

### 4.1 当前链路

```
user.define_outcome 到达 OMA
  └─ ❌ IsPublicWorkerInputEvent 白名单不含它（events.go:59-65）
       → 事件到不了沙箱 worker，且不重置 idle
       → 【writer 收到 outcome 后完全不会开始工作】

writer 一轮结束
  └─ ⚠️ 信号来源：worker 自报状态（codesessions/status.go:27-35）
       running               → session.status_running
       idle / requires_action → session.status_idle
       → 【requires_action（等权限确认）也被映射成 idle，朴素用它触发会误判】

grader
  └─ ❌ 完全不存在
```

### 4.2 与官方的契约差距

| 项 | 官方 | OMA 现状 |
|---|---|---|
| `outcome_evaluations[].result` | `pending/running/evaluating`+4 终态 | 只有 `status: "pending"`，**字段名都不对** |
| 资源字段 | `type,outcome_id,description,result,iteration,completed_at,explanation` | `{id,outcome_id,max_iterations,status,type,updated_at}` |
| `span.outcome_evaluation_*` | harness 产生 | 只注册了分类（`events.go:37`），**无产生方** |
| `session.outcome_evaluation_ended` webhook | "for a single iteration completed" | 收到 `define_outcome` 就发 |
| grader 引擎 | 有 | **无** |

### 4.3 沙箱与 environment-manager 的数量关系（现状）

| 关系 | 现状 |
|---|---|
| OMA 沙箱 : work item | 1 : 1（`environment_sandboxes` 按 `work_id` 建） |
| 沙箱 : `task-run` 进程 | 1 : 1 |
| `task-run` : Claude Code | 1 : 1（`cmd_task_run.rs:808-817`） |

即 **1 : 1 : 1**。

### 4.4 environment-manager-rs 的共享路径冲突（若要在同沙箱跑第二个 Claude Code）

**【代码】** 仓库: `/home/gyq/Coding/Agent/environment-manager-rs`（superduck-ai 自己的 Rust 仓库，**可改**）

以下路径**按沙箱固定**（不区分 session），两个 Claude Code 并存会互踩：

| 路径 | 问题 | 位置 |
|---|---|---|
| `/home/claude/.claude/remote/.session_ingress_token` | 常量路径，每次覆盖；**`destroy_inner` 会删除它** → 先结束的进程破坏仍在运行的另一个的 bash token | `claude_code_executor.rs:68`、`:1125-1128` |
| `claude mcp add/remove --scope user` | 改 Claude Code **用户级**配置；remove-before-add 有竞态 | `mcp/registry.rs:181-227` |
| `/tmp/claude-code.log` | 所有实例共用，只能追加、会交错 | `claude_code_executor.rs:65` |
| `/tmp/claude-command` | truncate 覆盖写 | `claude_code_executor.rs:1274-1292` |
| `~/.gitconfig`（`git config --global`） | 跨 session 共享的全局配置 | `manager.rs:617-632` |

**已具备的隔离**（无需改动）：
- session 锁**按 session ID 分**：`{TMPDIR}/environment-manager-{转义后 session_id}.lock`
  （`lockfile.rs:294-299`）→ **不同 session ID 的两进程不会互斥**
- 生产代码**无 pid 文件**；agent proxy / MCP / gitproxy **全部** bind `127.0.0.1:0` 随机端口
- 取消只杀 direct child，不杀 process group（`claude_code_executor.rs:9-10`，有测试验证）

**【代码】** 服务端侧的硬约束：worker epoch 是 **per-code-session** 的，第二个 worker 对同一
code session 重新 register 会 bump epoch，旧 worker 写入返回 409
（`docs/design/be/ccrv2/ccr-v2-epoch-design.md:11-15`）。
→ **grader 不能复用 writer 的 code session 身份。**

---

## 5. 候选方案（**空间开放，无推荐**）

> 初版在此处得出「B 是唯一成本可控选项」。该结论**已被审计击穿**（§9 D3）：
> 它依赖 §3 的必然性，而 §3 不成立；且方案空间被人为收窄——
> snapshot 方案在初版被基于错误前提一笔否决。

| 方案 | 内容 | 已知信息 |
|---|---|---|
| **A** | OMA 用 `Provider.RunCommand` 直接在沙箱 exec `claude` | 需在 OMA 侧重造 EM 已有的认证装配 / session URL 构造等。**未做完整评估** |
| **B** | EM 支持「在同一沙箱内再跑一个 Claude Code」，OMA 侧用新 code session 身份承载 | 前置代价见 §4.4（5 处共享路径 + epoch 冲突）。**能避开这些代价的替代方案未被充分评估** |
| **G'** | **snapshot → 新建沙箱**跑 grader | 初版基于错误前提否决。**需要重新评估**。已知真实缺点见下 |
| **J** | OMA 侧实现服务端 grader loop，工具调用打到 writer 的 sandbox | 需在 Go 侧实现工具执行层。**未评估** |

**G' 的真实缺点（审计补充，非初版的错误论证）**：
- writer 迭代间隙未必 idle；checkpoint 语义是 idle 后 30 天窗口
- 中途拍快照可能需先停 writer
- 快照会丢失 writer 仍在运行的活动进程/端口
- **OMA 当前没有 sandbox snapshot API**，落地需先补 provider 能力
- 保真度问题取决于 rubric 内容：若只评最终交付物可接受，若评工作树状态则不足

**关键未决**：官方 outcomes 是否支持 self-hosted（§2 第 4 条）**直接决定**方案空间——
若支持，官方 grader 必须能在「手脑分离」形态下工作，则 B 不可能是官方形态。

---

## 6. 官方 grader ≠ 官方 thread（结论成立，审计 STANDS）

### 6.1 事件与语义对照

| | 官方 thread | 官方 grader |
|---|---|---|
| 面向 | multiagent orchestration | outcome 评估 |
| 事件族 | `session.thread_created` / `session.thread_status_*` | `span.outcome_evaluation_*` |
| 生命周期事件 | 有 | 无 thread 事件 |
| 由谁发起 | 协调者运行时 spawn | **harness** provision |
| 文档交叉引用 | ❌ 互不提及 | ❌ 互不提及 |

### 6.2 证据（审计后补强）

**【原文】** `webhooks.md:24`

> `session.thread_created` ｜ New multiagent thread opened: an additional agent called by the coordinator is starting work, or the session's advisor is being consulted.

**【代码】** 审计补充了初版未发现的更强证据：
- `BetaManagedAgentsSessionRosterEntry` 的 discriminator 是**闭集** `agent | advisor`，
  `additionalProperties: false`——**没有 grader 类型**
- `session.thread_created` schema 描述：「Emitted when a subagent is spawned as a new thread」
- `GET /v1/sessions/{session_id}/threads` 的 `agent` 字段只允许 `SessionThreadAgent or Advisor`

> 若 grader 是一条 thread，它必须出现在上述闭集与列表 API 中——合同不支持这种可能。

### 6.3 设计约束

官方存在**第三类执行上下文**：既不是主 agent，也不是 multiagent thread，而是
**harness provision 的评估执行**（对外只暴露 `span.outcome_evaluation_*`）。

> **若用 OMA 的 thread 机制承载 grader，必须防止它泄漏到对外的 `GET /threads` 列表与
> `session.thread_*` 事件族中**——否则会产出官方契约里不存在的可观测行为。

### 6.4 OMA 的 thread **没有执行运行时**（结论成立，审计 STANDS）

| 事实 | 证据 |
|---|---|
| `internal/runtime/` + `internal/environments/`（14 个生产文件）中 `thread` 命中 = **0** | 三条独立命令验证 |
| `CreateSessionThreadIfAbsent` 全仓库仅 **2 个调用点**（`event_mapper.go:401`、`:461`），均为**懒创建** | `internal/db/sessions.go:298` |
| 子 thread ID 从 Claude Code Task 标识**确定性派生**：`sha256(codeSessionID + "\x00claude-task\x00" + key)` | `identity.go:16-23` |
| 真正的"创建"是事后投影：`mapper.go:571-599` 把 `task_started` 合成 `session.thread_created` | — |
| `session_threads` 表 **17 列，无** code_session/worker/sandbox 列 | `schema.go:1003-1026` |
| `code_sessions` 表 **22 列，无** thread 列 | `schema.go:1094-1121` |
| HTTP 层只有 list/get/archive/events/stream，**无创建入口** | `transport.go:66-72` |
| 输入事件白名单不含任何 `session.thread_*` | `events.go:48-55` |
| `multiagent` 只做声明校验，**不传 runtime** | `agents/handler.go:564-602` |

**实际行为**：子 agent 由 Claude Code 自己的 Task 工具在**同一进程内**执行，OMA 事后贴 thread 标签。
1 session ∶ 1 code session ∶ 1 sandbox ∶ 1 Claude Code 进程。

---

## 7. 待决问题（重排）

| # | 问题 | 状态 |
|---|---|---|
| **Q0** | **官方 outcomes 是否支持 self-hosted？** 这决定方案空间（见 §5 关键未决） | **阻塞性，未决** |
| Q1 | 承载第二个上下文：B / G'（snapshot）/ J（服务端 loop）/ 其他 | **空间开放，无推荐** |
| Q2 | 若走 B：EM 的 5 处共享路径做 session 隔离，是否接受为前置任务 | 待拍板 |
| Q3 | ~~OMA thread 是否有执行运行时~~ | ✅ 已核查：**没有** |
| Q4 | 触发信号：`session.status_idle` 把 `requires_action` 误判为「一轮结束」，如何区分 | 待设计 |
| Q5 | 基础设施故障（grader 超时 / 沙箱被回收）映射到哪个 result（官方 5 态无对应值） | 待设计 |

---

## 8. 引用索引

| 编号 | 文件 | 行 | 官方页面 |
|---|---|---|---|
| §1.1 | `docs/managed-agents-reference/define-outcomes.md` | 9, 11, 713 | https://platform.claude.com/docs/en/managed-agents/define-outcomes |
| §1.2 | `docs/managed-agents-reference/tools.md` | 642 | https://platform.claude.com/docs/en/managed-agents/tools |
| §1.3 | `docs/managed-agents-reference/self-hosted-sandboxes.md` | 39, 48 | https://platform.claude.com/docs/en/managed-agents/self-hosted-sandboxes |
| §1.4 | `docs/managed-agents-reference/multiagent-orchestration.md` | 17, 21 | https://platform.claude.com/docs/en/managed-agents/multiagent-orchestration |
| §1.5 | `docs/managed-agents-reference/events-and-streaming.md` | 2379, 2382 | https://platform.claude.com/docs/en/managed-agents/events-and-streaming |
| §1.6 | `docs/managed-agents-reference/define-outcomes.md` | 565-627 | 同上 |
| §1.6 | `docs/managed-agents-reference/webhooks.md` | 27 | https://platform.claude.com/docs/en/managed-agents/webhooks |
| §6.2 | `docs/managed-agents-reference/webhooks.md` | 24 | https://platform.claude.com/docs/en/managed-agents/webhooks |
| §6.2 | `docs/api-reference/beta/sessions/threads.md` | — | https://platform.claude.com/docs/en/api/beta/sessions/threads |
| §1.6 | `openapi/oma.en.json` | schema | `BetaManagedAgentsOutcomeEvaluationResource` 等 7 个 |
| §4.4 | `environment-manager-rs` | 见表格 | https://github.com/superduck-ai/environment-manager-rs |

> 除 `openapi/oma.en.json` 与本仓库代码外，上表 `docs/managed-agents-reference/*` 均位于
> **`docs/api-gap` 分支**，不在本分支。

---

## 9. 独立审计记录（2026-09-16）

**方法**：5 个独立审查 agent（Haiku）分别复核官方文档类、OMA 代码类、两个 EM 仓库类、
仓库流程类 claim，以及**对抗性攻击本文档的结论**。共 **60 条 claim**。

**结果**：54 条通过，**2 条判错**，**4 条结论被击穿**。

### 9.1 被击穿的结论（本文档已据此改写）

| 原结论 | 裁定 | 击穿点 |
|---|---|---|
| **grader 与 writer 同沙箱** | `WEAKENED` | P2 过度解读、P3 沉默论证、P4 被 idle checkpoint 原语打破（§3.2） |
| **grader 必须与 writer 共用同一套工具执行面** | `FALLS` | 该句在 web 过滤小节；被减的两个工具是**服务端**工具，对沙箱工具面零信息；官方 `multiagent:21` 反证「同沙箱 ≠ 共享工具」 |
| **所以选方案 B** | `FALLS` | 依赖上两条的必然性；且方案空间被人为收窄（snapshot 方案被坏前提否决） |
| **OMA 没走偏（用 HTTPS_PROXY 论证）** | `WEAKENED` | **`HTTPS_PROXY` 论证是错的**：`agent_proxy.rs:322` 把 API host 放进 `NO_PROXY`，模型请求不走该代理。真实机制是 `ANTHROPIC_BASE_URL` 指向 OMA API server（见 `docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md:18-25`） |

### 9.2 站住的结论

| 结论 | 裁定 | 补强 |
|---|---|---|
| **官方 grader 不是 session thread** | `STANDS` | 审计补了 roster 闭集、thread_created schema、list API 三类证据（§6.2） |
| **OMA thread 无执行运行时** | `STANDS` | 审计明确否掉了「经 code session 间接承载」这条反击 |

### 9.3 审计发现的事实性笔误（已修正）

| 位置 | 原表述 | 修正 |
|---|---|---|
| §1.1 | 把「Anthropic-managed 沙箱的产物路径」标到 `define-outcomes.md:713` | 该行只有 "inside the sandbox"，无 "Anthropic-managed" |
| §4.4 | 称官方 Go EM 与 Rust EM「做的是同一件事」 | 结论方向成立（都让 Claude Code 自己出网），但机制不同：Rust 是 EM 注入 `HTTPS_PROXY`，Go 是 Claude 侧 upstreamproxy 读 token 文件 |
