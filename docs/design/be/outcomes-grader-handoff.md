# Handoff：Outcome Grader 实现（Issue #246）

> 写给接手的 agent。本文假设你**没有任何前序对话上下文**。
> 生成日期: 2026-09-16 ｜ 前置 agent 已因推理质量问题被停用

---

## 0. 先读这一节

**你要做的功能**：在 OMA（Open Managed Agents）里实现 Anthropic Managed Agents 的
**outcome grader 引擎**——即 `user.define_outcome` 发出后，系统按 rubric 自动评分、
把反馈回灌给 writer、迭代直到满足或耗尽 `max_iterations`。

**当前状态**：**核心架构决策未定，且前序 agent 的推荐方案已被审计证伪。**
不要直接开始写代码。先解决 §4 的阻塞问题。

**前序 agent 的错误模式**（请引以为戒，详见 §6）：
> 在证据等级不足时用确定的语气下结论；把推断写成事实；被质疑后转向，再犯同样错误。
> 最终它给出的"最推荐方案"经独立审计后，其两条核心推理被判定为不成立。

---

## 1. 仓库与分支状态

| 仓库 | 路径 | 说明 |
|---|---|---|
| **OMA** | `/home/gyq/Coding/Agent/open-managed-agents` | 主仓库 |
| **environment-manager（Rust）** | `/home/gyq/Coding/Agent/environment-manager-rs` | superduck-ai 自己的沙箱侧管理器，**可修改** |
| **environment-manager（Go，恢复版）** | `/home/gyq/Coding/Agent/environment-manager` | 从 Anthropic 二进制 DWARF 恢复出来的参考实现，**不可改**，仅作对照 |

**当前分支**：`fix/outcomes-link-and-grader`（跟踪 `postroggy/fix/outcomes-link-and-grader`）

上手先跑：
```bash
cd /home/gyq/Coding/Agent/open-managed-agents
git branch --show-current        # 应为 fix/outcomes-link-and-grader
git log --oneline main..HEAD     # 应看到 outcome 链路修复 + 3 个文档提交
git status                       # 应为 clean
```

**该分支已完成的工作**（提交 `00c81de0`）：
- `user.define_outcome` 的 rubric 校验（text/file 两种）、`max_iterations` 整数与范围校验
- outcome evaluation 的**完整存储**（此前只存摘要）
- `sessionConfig.outcomes` 透传（**注意：该通道语义有问题，见 §4**）
- `session.outcome_evaluation_ended` 注册进 `CategoryFor`
- deployment 路径对齐

**对应 PR**：`#309`，OPEN、非 draft、CI 全绿。**但 CodeRabbit 明确指出它未覆盖 grader 引擎。**

**该分支已 rebase 到 main**（`3ccb8094`）。rebase 后修复了两处 main 的内部函数签名变更导致的测试编译失败
（`prepareDeploymentExecution` 增加 `runtimeUserUUID` 参数；`managedAgentSessionConfig` 改为返回 `(raw, error)`）。
**这两处是 OMA 自己的内部变更，不是官方 API 变更。**

---

## 2. 必读文档

| 文档 | 位置 | 内容 |
|---|---|---|
| **调研与证据** | `docs/design/be/outcomes-grader-research.md` | 官方原文引用、已证伪结论、独立审计记录。**先读这份** |
| **原链路设计** | `docs/design/be/outcomes-link-and-grader.md` | PR #309 的设计文档。⚠️ 其「范围边界」一节关于 `environment-manager-rs` 的修复方向**已被证伪**，见 §4 |
| **官方文档原始镜像** | **`docs/api-gap` 分支** → `docs/managed-agents-reference/`（27 页） | 引用官方原文**必须**用这份，不要用 `docs/en/` |
| 官方 OpenAPI | `openapi/oma.en.json` | outcome 相关 schema 共 7 个 |
| 官方 API reference | **`docs/api-gap` 分支** → `docs/api-reference/beta/` | 端点级参数/响应 |

> **⚠️ 文档来源纪律**：`docs/en/` 与 `docs/zh/` 是 **OMA 改写版**（把 "Anthropic" 替换成 "OMA"），
> **且已知有内容丢失**——官方 `tools.md:642` 的 grader 工具集契约在改写版里完全没有。
> 引用官方原文一律用 `docs/api-gap` 上的镜像。

读取方式：`git show docs/api-gap:docs/managed-agents-reference/<file>`

---

## 3. 已确证的事实（可直接使用，均已独立审计复核）

### 3.1 官方契约

- grader 由 **harness** provision，使用**独立 context window**，产出 explanation 回灌 agent
  （`define-outcomes.md:9,11`）
- grader **有**工具，但**没有** `web_search` / `web_fetch`（`tools.md:642`）。
  ⚠️ **不要**从这句推出"grader 工具集派生自 session"——该句在 web 域名过滤小节，
  且被减的两个工具**跑在服务端**，对沙箱工具面零信息
- `span.outcome_evaluation_*` 三个事件的 required 字段与 `result` 语义（见调研文档 §1.6）
- **两套 result 不同**：span 的 `result` 含 `needs_revision`；资源的 `result` 不含
  （只有 `pending`/`running`/`evaluating` + 4 个终态）
- `outcome_evaluations[]` 资源 required 字段：
  `type, outcome_id, description, result, iteration, completed_at, explanation`
- `session.outcome_evaluation_ended` 语义 = **单次迭代评估完成**（不是"收到定义"）
- 官方支持「一沙箱 + N 个上下文隔离执行」（thread 模型），但 **grader 不是 thread**
  （证据：`session.thread_created` 只由协调者委派/advisor 触发；roster 是闭集 `agent|advisor`；
  threads 是可枚举的公开资源）

### 3.2 OMA 现状

- **`user.define_outcome` 到不了沙箱 worker**：`IsPublicWorkerInputEvent`
  白名单（`internal/managedagentsevents/events.go:59-65`）不含它 →
  writer 收到 outcome 后**完全不会开始工作**，且不重置 idle
- `span.outcome_evaluation_*` **已注册分类**（`events.go:37`）但**无产生方**
- `outcome_evaluations` 记录用 `status` 而非官方 `result`，且缺 `iteration`/`completed_at`/`explanation`
- `session.outcome_evaluation_ended` webhook 在**收到 define_outcome 时**就发，时机错误
- `session.status_idle` 由 worker 自报状态驱动，**`requires_action` 也被映射成 idle**
  （`internal/codesessions/status.go:27-35`）→ 朴素的"一轮结束"触发会误判
- **OMA 的 session thread 没有执行运行时**：纯事件归因 + 展示，
  `internal/runtime/` 与 `internal/environments/` 中 `thread` 命中为 0
- 沙箱与 EM 是 **1:1:1**（1 work → 1 sandbox → 1 `task-run` → 1 Claude Code）

### 3.3 environment-manager-rs 的约束（若要同沙箱跑第二个 Claude Code）

- **5 处按沙箱固定的共享路径**会互踩，其中最严重的是
  `/home/claude/.claude/remote/.session_ingress_token`——**`destroy_inner` 会删除它**，
  先结束的进程会破坏仍在运行的另一个（`claude_code_executor.rs:68`、`:1125-1128`）
- 而 session 锁**是按 session ID 分的**（`lockfile.rs:294-299`），不同 session ID 不互斥
- 生产代码**无 pid 文件**，agent proxy / MCP / gitproxy 全部 bind 随机端口
- **服务端 worker epoch 是 per-code-session**：第二个 worker 对同一 code session
  register 会 bump epoch，旧 worker 写入返回 409 → **grader 不能复用 writer 的 code session 身份**

---

## 4. 阻塞问题与已证伪的路径

### 🚧 Q0（阻塞性，未决）

> **官方 outcomes 是否支持 self-hosted 环境？**

官方文档**从未声明**（已审计确认是"没写"，不是"写了不支持"）。
这一条**直接决定方案空间**：

- 若**支持** → 官方 grader 必须能在「手脑分离」形态下工作
  （self-hosted 的 worker 只接收来自 Anthropic 的工具执行请求），
  则「在同沙箱跑第二个 Claude Code」不可能是官方形态
- 若**不支持** → outcome 只存在于 cloud/沙箱内 loop 的形态，方案空间收窄

**这是接手后应该优先解决的问题。**

### ❌ 已证伪的路径（不要重复走）

| 路径 | 为什么不行 |
|---|---|
| **"grader 与 writer 同沙箱"作为必然结论** | 该推理链的三步均被击穿（过度解读、沉默论证、被 idle checkpoint 原语打破）。它最多只是"更可能"，不是必然 |
| **"grader 必须与 writer 共用同一套工具实现"** | 推不出来。官方 `multiagent-orchestration.md:21` 明确「Tools, MCP servers, and context are **not shared**」，同沙箱 ≠ 共享工具实现 |
| **用 `HTTPS_PROXY` 论证 Claude Code 在沙箱内调模型** | **事实错误**。`agent_proxy.rs:322` 把 API host 放进 `NO_PROXY`，模型请求不走该代理。真实机制是 `ANTHROPIC_BASE_URL` 指向 OMA API server（见 `docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md:18-25`） |
| **`environment-manager-rs` 的修复方向** | 原设计文档称「需 EM 侧适配 `type` 判别扩展」——**不可行**。`OutcomeField` 结构体只有 `outcome_type` + `git_info` 两个字段，**没有任何可承载 rubric 的字段**，Go 与 Rust 两个仓库都是如此。该通道语义是 **git 交付**（"期望的 git outcomes"），与 rubric 评分无关 |
| **rubric 经 `sessionConfig.outcomes` 透传** | PR #309 做了这件事，但该通道结构上装不下 rubric。官方 OpenAPI 中**没有任何** session/environment/sandbox 配置层面的 schema 带 `outcomes` 属性——即官方**根本没有设计这条通道** |

### ⭕ 仍然开放的方案（**无推荐，需重新评估**）

| 方案 | 说明 |
|---|---|
| **B** | EM 支持在同一沙箱内再跑一个 Claude Code；OMA 侧用新 code session 身份。代价见 §3.3 |
| **G'** | 从 writer 沙箱做 **snapshot → 新建沙箱**跑 grader。已知真实缺点：writer 迭代间隙未必 idle、中途拍快照可能要停 writer、快照丢失活动进程、**OMA 当前无 sandbox snapshot API 需先补 provider 能力**、保真度取决于 rubric 内容 |
| **J** | OMA 侧实现服务端 grader loop，工具调用打到 writer 的 sandbox。需在 Go 侧实现工具执行层 |
| **A** | OMA 用 `Provider.RunCommand` 直接 exec。需重造 EM 已有的认证/URL 构造等，**未做完整评估** |

---

## 5. 建议的接手顺序

1. **读** `docs/design/be/outcomes-grader-research.md`（尤其 §3 已证伪记录与 §9 审计记录）
2. **确认** Q0：查官方文档中 outcomes 与 self-hosted 的关系。注意 `docs/api-gap` 镜像的
   `scheduled-deployments.md`、`reference.md` 里有 `self_hosted` 与 `define_outcome` 同时出现的位置，
   但那只是分列在不同条目，**不构成支持声明**
3. **补评估** §4 的四个候选方案，特别是 G'（snapshot）——它被前序 agent 基于错误前提否决过
4. **在动代码前，把方案选择交回给用户拍板**，并说明每个方案的取舍

---

## 6. 前序 agent 的错误模式（务必避免）

用户在本次交接前明确表示不信任前序 agent。其错误有共同模式，请主动规避：

| 错误 | 实例 |
|---|---|
| **把推断写成事实** | "grader 与 writer 同沙箱"（推断）被写成像官方明文规定 |
| **沉默论证** | 用"官方文档没提限制"论证"官方支持两种环境" |
| **过度解读原文** | 把 "land wherever the agent writes them"（不强制路径）读成"不可枚举" |
| **用错证据** | 用 `HTTPS_PROXY` 论证模型调用路径，而该代理恰恰排除了 API host |
| **把转述当一手核查** | 转述子 agent 摘要而未验证原始来源 |
| **方案空间人为收窄** | 基于坏前提一笔否决 snapshot 方案 |
| **被质疑即转向** | 每轮修正都建立在新的不完整证据上，形成"反复下结论"的印象 |

**建议的纪律**：

> 每条陈述标注证据等级——**【原文】/【代码】/【推断】/【假设】**——并写明推断的承重点与失效条件。
> 不要把【推断】写成【原文】的语气。不确定就说不确定。

---

## 7. 关键文件索引

| 用途 | 路径 |
|---|---|
| grader 证据与审计 | `docs/design/be/outcomes-grader-research.md` |
| 原链路设计（部分已过时） | `docs/design/be/outcomes-link-and-grader.md` |
| 官方文档镜像（`docs/api-gap` 分支） | `docs/managed-agents-reference/` |
| 官方 API reference（`docs/api-gap` 分支） | `docs/api-reference/beta/` |
| outcome 输入校验与归一化 | `internal/sessions/service_helpers.go` |
| worker 输入事件白名单 | `internal/managedagentsevents/events.go` |
| worker 状态 → session 状态映射 | `internal/codesessions/status.go` |
| 沙箱启动配置 | `internal/environments/environment_manager.go` |
| E2B 沙箱 provider | `internal/runtime/e2bruntime/runtime.go` |
| ccrv2 epoch 设计 | `docs/design/be/ccrv2/ccr-v2-epoch-design.md` |
| 模型运行时与 upstream proxy | `docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md` |

---

## 8. 关于本分支未提交的改动

截至交接时，`fix/outcomes-link-and-grader` 上有 **3 个文档提交**（调研文档 + 两次修订），
以及 §1 描述的链路修复提交。工作区应为 clean。

**注意**：该分支已 rebase 到 main，**与远端 `postroggy/fix/outcomes-link-and-grader` 已分叉**，
后续推送需要 force-push。**推送前必须确认目标 remote，禁止直接推到主仓库 `origin`。**
