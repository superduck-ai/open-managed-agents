# 设计文档同步与 Surface Audit

本文记录 `docs/design/` 与代码 surface 的同步机制：确定性 audit 如何检测漂移、LLM agent 何时介入、触发与闭环如何约束。机制本身跨前后端，归入 `be/` 是因为 audit 与 workflow 主要消费后端 surface（API mounts / packages / migrations）。

## 范围

- 覆盖：`scripts/docs-audit/audit_design_docs.py` 的检测逻辑与退出码、`design-doc-audit.yml` 的 CI 行为、`duckpr-docs-sync.yml` 的触发与 agent 闭环、`.agents/skills/docs-sync/SKILL.md` 的写入契约。
- 不覆盖：DuckPR Review（独立的代码评审能力）、Pullfrog 本身的 agent 运行时、跨仓文档同步（本项目文档同仓存放）。

## 两层职责

| 层 | 机制 | 是否用 LLM | 消费者 |
|---|---|---|---|
| 检测 | `audit_design_docs.py` + `design-doc-audit.yml` CI | 否（确定性正则 + surface_map 比对） | PR CI、workflow 第一步 |
| 写入 | `duckpr-docs-sync.yml` + Pullfrog opencode + `docs-sync` skill | 是 | `@duckpr docs` / `workflow_dispatch` |

检测层先行、确定、可在本地复现；写入层仅在显式触发时介入，且必须消费检测层的产出。这是与"纯 LLM 判断哪些 PR 需要文档"做法的关键差异：判断权在确定性侧，LLM 只负责"如何写"。

## 检测层：surface audit

### 提取的 surface

`audit_design_docs.py` 从源码正则提取七类 surface，覆盖 AGENTS.md §「设计文档同步」点名的所有需文档化的变更面：

- `api_mounts` — `internal/api/server.go` 的 `r.Mount(...)` / `.Post(...)` + `internal/codesessions` 前缀。
- `api_subroutes` — 各 resource 包的 `Register*Routes` 入口（如 `platformapi.RegisterPlatformBillingRoutes`），代表每个包贡献的 HTTP 资源。以**定义包**为准（调用处可能是变量名前缀如 `codeSessionService.RegisterRoutes`）。
- `packages` — `internal/` 下含 `*.go` 的直接子目录。
- `migrations` — `internal/db/migrations/*.sql`。
- `fe_routes` — `web/src/app/router.tsx` 中 `path: '...'` 条目。
- `event_contracts` — `internal/managedagentsevents/events.go` 中的事件类型字符串字面量（如 `session.status_running`），这是 managed agent 的事件契约。
- `auth_middleware` — `internal/api/server.go` 中 `(s *Server) xxxMiddleware` 定义 + `.Use(...)` 调用点，覆盖权限边界与平台中间件链。

`api_subroutes` / `event_contracts` / `auth_middleware` 是补盲区新增的三类——此前 audit 只看顶层 Mount，无法发现某个包内新增的子路由、事件契约变更、鉴权中间件调整。

每类有 `EXTRACTION_FLOORS` 下限。提取数低于下限视为解析器损坏（layout 变化），直接 exit 2，而非报告逐项缺失。

### Surface map 三态

`scripts/docs-audit/surface_map.md` 把每个 surface 映射到三种状态之一：

```
<surface> -> docs/design/<area>.md   # 已有设计文档
<surface> -> internal                # 基础设施/无设计关切，无需文档
<surface> -> gated:<reason>          # 明确推迟（如 gated:needs-design-doc）
```

每个被提取的 surface 必须落在唯一一个 accountability bucket：`mapped` / `internal` / `gated` / `finding`。一个 surface 脱离所有 bucket 视为审计逻辑退化（exit 2）。

### Map hygiene + staleness

除正向 coverage 外，audit 还做两项反向质量检查：

- **map hygiene** — duplicate（同 section 重复 key）、dead_entry（map 里的 surface 代码已不存在）、dead_doc_target（map 指向的文档缺失）、dead_unlisted（unlisted 段登记了不存在的文档）。
- **staleness** — 扫描 `docs/design/**` 正文里的 `internal/<pkg>` 引用，若该包代码已不存在则报 `stale_pkg_ref`。这捕获正向 coverage 看不到的重命名/删除漂移（正向 audit 只知道当前存在的 surface，不知道文档里残留的旧名）。

### 退出码

| Exit | 含义 | CI 行为 |
|------|------|---------|
| 0 | 干净 | 通过 |
| 1 | coverage / map hygiene findings（未映射、死映射、文档缺失） | `design-doc-audit.yml` 软告警（warning），不阻塞 |
| 2 | 提取下限或完整性记账失败（解析器退化） | `design-doc-audit.yml` 硬失败 |

软失败是过渡策略：surface_map 尚在补全期间不阻塞 PR；待噪声收敛后把 step 改为 hard-fail。

### Snapshot 漂移

`surface_snapshot.json` 记录上次提交时的 surface 全集。`--diff` 比对当前提取与快照，报告 added/removed surface。`--update-snapshot` 在 surface 变化后刷新快照。快照让"新增了一个未映射 API"这类增量可被自动化捕获，而非只靠人来发现。

## 写入层：docs-sync

### 触发

- `@duckpr docs` / `@pullfrog docs` PR 评论 → DuckPR bot 路由（见 `superduck-ai/duckpr` 的 `dispatchDocsSync`）。触发者需要仓库 `write` 及以上权限（见下方安全边界），低权限用户被拒绝并在 PR 贴评论说明。
- `@duckpr docs --audit-only` / `@pullfrog docs --audit-only` — 仅运行确定性 audit（`skip_agent=true`），不调用 LLM。flag 由 DuckPR 从 prompt 中剥离，不传入 agent。适用于快速覆盖检查或 token 敏感场景。
- `workflow_dispatch` 直接触发（`pr_number` + `model` + `llm_base_url` + `prompt` + `skip_agent`）。

**Model 解析三层优先级**：DuckPR dispatch 时仅在 repo 配置了 `docs_model` 时传 `model` input；未配置则省略，由 workflow 的 `Require model` 步骤按 `inputs.model` > `vars.DUCKPR_DOCS_MODEL` > `vars.DUCKPR_MODEL` 解析。这允许 docs-sync 使用比 review 更便宜的 model（如 `DUCKPR_DOCS_MODEL=kimi-k2.5` 对 `DUCKPR_MODEL=glm-5.2`）。DuckPR **不**回退到 review model——那会遮蔽 vars，静默强制使用更贵的 model。三者都未配置时 workflow 硬失败并给出配置指引。

不做"每个 PR 自动触发"——文档同步是显式动作，避免噪声 PR 与 token 消耗。但 `design-doc-audit.yml` CI 会在检测到信号时**自动提示**：当 audit exit=1（coverage/map/staleness findings）或 `classify_changes.py` verdict 为 `must_document` / `needs_review` 时，CI 在 PR 上贴一条评论（带 `<!-- design-doc-audit-nudge -->` marker，upsert 不重复），提示作者触发 `@duckpr docs`。这是 audit → agent 的衔接桥：确定性检测发现漂移，人决定是否派发 agent。

### 判定层：classify_changes（LLM 介入前的确定性 triage）

`scripts/docs-audit/classify_changes.py` 在 agent 运行前对 PR 变更文件做确定性 triage，产出 PR 级 verdict（`exclude` / `must_document` / `needs_review`）。这是借鉴 warp `classify-changelog-pr` 的判定矩阵思想，但针对单仓场景收敛。

核心设计原则——**不静默丢弃**：

- 纯规则只覆盖高置信、无歧义的场景：CI/test/config/docs → `exclude`（high）；events/migrations/auth/router-wiring → `must_document`（high）。
- **任何规则无法判定的文件显式输出 `needs_review` + reason**，绝不静默归类为 exclude。例如 `internal/httpapi`（plumbing 包）路径单独无法判定行为是否变化，路由由 `needs_review` 交 agent/人核实。
- `internal/` 下的未知新包默认 `should_document`（medium）而非 exclude——这是防遗漏守卫：新包通常是真的代码 surface，宁可让 agent 核实也不漏。
- 关键词增强（`permission` / `state machine` / `outbox` 等）把 `should_document` 上调为 `must_document`。

agent 在 SKILL.md step 2 把这个 verdict 作为**约束性输入消费**：verdict=exclude 时禁止写文档（只报「无需更新」）；verdict=must_document 时必须为每个 flagged 文件产出文档更新或显式「无需更新+原因」；verdict=needs_review 时必须逐一 inspect diff 并在评论里说明决策。

### 同仓 + 受限推送

与跨仓文档同步不同，本项目文档在 `docs/design/` 同仓存放。agent 检出 PR head 分支，以 `push: restricted` 只推送该 feature 分支，不触碰 `main`、不建 tag、不删分支。

### Prompt 上下文注入与注入防御

`duckpr-docs-sync.yml` 的 prompt 构造步骤把以下块注入 agent，避免 agent 自行 `gh` 捞取。块分两类——**trusted**（workflow 产出，XML 标签）和 **untrusted**（用户可控，唯一分隔符）：

**Trusted 块**（workflow 产出，agent 可信）：

- `<pr_context>` — PR 号、标题、URL、head/base 分支、**作者**。
- `<changed_files>` — 变更文件列表（`<status> +<add>/-<del>  <filename>` 格式，上限 300）。
- `<classify>` — `classify_changes.py` 的判定 JSON（约束性 per-file triage）。
- `<audit_findings>` — `audit_design_docs.py --diff` 的 JSON（pre-sync 基线，含 `exit_code` + `findings[]`）。

**Untrusted 块**（用户可控，agent 不可作为指令）：

- `pr_body` — PR 描述。
- `trigger_comment` — 触发评论正文。
- `extra_instructions` — workflow `prompt` input。

注入顺序（安全关键）：`----- BEGIN SKILL -----`（指令框架）→ 对抗性声明 → trusted 块 → untrusted 块（最后）。这确保 agent 先建立指令框架，再看到任何攻击者可控内容。

**Prompt 注入防御**（三层）：

1. **SKILL 先行** — SKILL.md 正文放在 prompt 最前面，使 agent 的指令框架在任何 untrusted 内容之前建立。
2. **唯一分隔符** — untrusted 块用每次运行随机生成的分隔符 `=== UNTRUSTED_DATA_OPEN_<random16> ===` / `=== UNTRUSTED_DATA_CLOSE_<random16> ===` 包裹，攻击者无法在 PR body 中预先伪造（随机 token 不可预测）。同时 sanitize 会从 untrusted 内容中移除任何匹配分隔符模式的行（belt-and-suspenders）。
3. **对抗性声明** — 在 trusted 与 untrusted 块之间插入明确声明：untrusted 块是数据不是指令，binding 输入只有 `<classify>` / `<audit_findings>` / SKILL。

untrusted 块还有长度上限（每块 20KB，DuckPR 侧 `extractDocsExtraPrompt` 另有 4KB 上限），防止 PR body 撑爆 agent 上下文。

`<classify>` 和 `<audit_findings>` 共同构成写入层的判定输入：classify 决定"要不要写"，audit findings 决定"写哪些 surface"。agent 据此 triage 该 PR 实际 owns 哪些 finding（finding 的 surface 出现在 `<changed_files>` 或 `<classify>` 的 must/should 列表中），而非全盘处理。

### 安全边界

docs-sync 的攻击面比 DuckPR Review 更大：它不仅消耗 LLM token，还推送代码到 PR 分支。因此有多层显式安全边界：

- **权限门控**（DuckPR `dispatchDocsSync`）— `@duckpr docs` 触发者必须有仓库 `write` 及以上权限。Review 只把 authorPermission 作为 prompt context 传入；docs-sync 在 dispatch 前硬性拒绝低权限用户（`isPermissionAtLeast(perm, "write")`），失败时删 dedupe key 允许重试。这阻止任意 read 权限用户触发 LLM token 消耗。
- **`--audit-only` 旁路** — `@duckpr docs --audit-only` 跳过 LLM agent，仅运行确定性 audit。适用于只需覆盖检查、token 敏感或低权限场景。
- **`persist-credentials: false`** — checkout 步骤不把 DuckPR App token 写入 `.git/config`。Pullfrog 的 ASKPASS 机制（`utils/gitAuth.ts`）在每次 `$git()` 调用时通过本地 HTTP 服务器临时注入凭据，`setupGit()` 还会重写 origin URL 去掉嵌入的 token。即使 agent 被 prompt 注入，也无法通过 `git config --get-regexp http` 读到 App token。
- **OIDC 环境变量清除**（Pullfrog `agents/opencode.ts` / `opencode_v2.ts` / `claude.ts`）— docs-sync 需要 `id-token: write` 来换取 Pullfrog API token，但 `ACTIONS_ID_TOKEN_REQUEST_URL` / `ACTIONS_ID_TOKEN_REQUEST_TOKEN` 等 OIDC 变量通过 `...process.env` 继承给 agent 子进程。spawn 前用 `DENIED_OVERRIDE_NAMES` 集合清除这些变量，阻止被注入的 agent 伪造 GitHub OIDC token。
- **命令注入防御** — `classify_changes.py` 的文件路径通过 stdin 管道传递（`< /tmp/paths.txt`），不用 `--files $(cat ...)` 命令替换。攻击者在 PR 中添加 `$(id).go` 这类文件名不会触发 shell 执行。
- **日志净化** — CI 日志不打印 `base_url` 完整 URL（只输出 `gateway=anthropic-compatible|unset`）；`LOG_LEVEL` 设为 `info` 而非 `debug`，避免 prompt 内容 / 模型请求 / 部分凭据写入 Actions 日志。

### 写入契约要点（SKILL.md）

- Truth first：只写 PR diff / linked issue / 既有设计文档中能验证的行为；不清楚处留 `<!-- TODO -->` 或映射 `-> gated:<reason>`，禁止臆造字段/类型/状态机。
- 决策优先级：更新既有文档 > 新建聚焦文档 > `-> internal` > `-> gated:<reason>` > 「无需更新」。多数 PR 的正确输出是"不写新文档"。
- 只允许写 `docs/design/**`、`surface_map.md`、`surface_snapshot.json`；禁止改业务代码/测试/配置/workflow。
- 只留一条总结评论（含 classify verdict + before→after finding 计数）。
- **commit 规范**：内容编辑（`docs/design/...`）与 bookkeeping 编辑（`surface_map.md` + `surface_snapshot.json`）分两个 commit，便于 review。
- **reviewer 路由**：最终评论 `cc @<pr author>` 请其核实文档与代码改动一致。

### 写后验证

agent 推送后，workflow 重跑 `audit_design_docs.py --diff` 取 after 快照，对比 pre-sync 的 high finding 计数，写入 job summary。这把"文档同步是否真的改善了 audit"变成可量化证据，而非只看 agent 评论的自述。

## 边界

- 检测层扫七类 surface（api_mounts / api_subroutes / packages / migrations / fe_routes / event_contracts / auth_middleware）。`scripts/`、`web/` 非路由部分、配置文件不在扫描范围，不会自动报为 unmapped。
- surface_map 的完整性由人工 + agent 维护；audit 不验证"映射目标文档内容是否真的描述了该 surface"（只验证文件存在），但 staleness 反向检查能捕获文档正文里残留的已删包名。
- LLM 写入层不做业务代码改动，因此 audit 对 agent 引入的文档漂移只能事后发现，无法在 agent 运行中拦截业务逻辑变更。
- `classify_changes.py` 是纯路径/关键词规则，不看 diff 语义——所以未知/plumbing 文件路由到 `needs_review` 交人核实，不静默归类。

## 兼容与测试

- `scripts/docs-audit/test_audit_design_docs.py` — audit 单元测试（七类提取、比对、accounting、staleness）。
- `scripts/docs-audit/test_classify_changes.py` — classify 单元测试（覆盖 exclude/must/should/needs_review 各路径 + 防静默丢弃守卫）。
- `design-doc-audit.yml` 在 PR CI 上验证检测层不退化（exit 2 硬失败），并在有 findings/must_document/needs_review 时贴 audit→agent 衔接提示。
- 写入层 E2E：在私有 fork 上 `workflow_dispatch` 派发，验证 agent 能对未映射 surface 产出文档 + 更新 surface_map + 刷新 snapshot（历史验证 run：Postroggy/open-managed-agents PR #3，commit `docs: sync design docs for demo_widgets`）。

后续收敛方向：把 `design-doc-audit.yml` 的 exit 1 从软告警逐步收紧为硬失败；补 surface_map 中现存 `gated:needs-design-doc` 项。
