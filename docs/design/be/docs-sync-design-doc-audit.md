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

`audit_design_docs.py` 从源码正则提取四类 surface：

- `api_mounts` — `internal/api/server.go` 的 `r.Mount(...)` / `.Post(...)` + `internal/codesessions` 前缀。
- `packages` — `internal/` 下含 `*.go` 的直接子目录。
- `migrations` — `internal/db/migrations/*.sql`。
- `fe_routes` — `web/src/app/router.tsx` 中 `path: '...'` 条目。

每类有 `EXTRACTION_FLOORS` 下限。提取数低于下限视为解析器损坏（layout 变化），直接 exit 2，而非报告逐项缺失。

### Surface map 三态

`scripts/docs-audit/surface_map.md` 把每个 surface 映射到三种状态之一：

```
<surface> -> docs/design/<area>.md   # 已有设计文档
<surface> -> internal                # 基础设施/无设计关切，无需文档
<surface> -> gated:<reason>          # 明确推迟（如 gated:needs-design-doc）
```

每个被提取的 surface 必须落在唯一一个 accountability bucket：`mapped` / `internal` / `gated` / `finding`。一个 surface 脱离所有 bucket 视为审计逻辑退化（exit 2）。

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

- `@duckpr docs` / `@pullfrog docs` PR 评论 → DuckPR bot 路由（见 `superduck-ai/duckpr` 的 `dispatchDocsSync`）。
- `workflow_dispatch` 直接触发（`pr_number` + `model` + `skip_agent`）。

不做"每个 PR 自动触发"——文档同步是显式动作，避免噪声 PR 与 token 消耗。`design-doc-audit.yml` CI 在每个触及 `internal/**` 的 PR 上跑检测层，但只告警、不自动派发 agent。

### 同仓 + 受限推送

与跨仓文档同步不同，本项目文档在 `docs/design/` 同仓存放。agent 检出 PR head 分支，以 `push: restricted` 只推送该 feature 分支，不触碰 `main`、不建 tag、不删分支。

### Prompt 上下文注入

`duckpr-docs-sync.yml` 的 prompt 构造步骤把以下块注入 agent，避免 agent 自行 `gh` 捞取：

- `<pr_context>` — PR 号、标题、URL、head/base 分支。
- `<pr_body>` — PR 描述。
- `<changed_files>` — 变更文件列表（status + +/- + 路径，上限 300）。
- `<audit_findings>` — `audit_design_docs.py --diff` 的 JSON（pre-sync 基线）。
- `<trigger_comment>` / `<extra_instructions>` — 触发评论与额外指令。
- `----- BEGIN SKILL -----` / `----- END SKILL -----` — `docs-sync/SKILL.md` 正文。

`audit_findings` 是写入层的判定输入：agent 据此 triage 该 PR 实际 owns 哪些 finding，而非全盘处理。

### 写入契约要点（SKILL.md）

- Truth first：只写 PR diff / linked issue / 既有设计文档中能验证的行为；不清楚处留 `<!-- TODO -->` 或映射 `-> gated:<reason>`，禁止臆造字段/类型/状态机。
- 决策优先级：更新既有文档 > 新建聚焦文档 > `-> internal` > `-> gated:<reason>` > 「无需更新」。多数 PR 的正确输出是"不写新文档"。
- 只允许写 `docs/design/**`、`surface_map.md`、`surface_snapshot.json`；禁止改业务代码/测试/配置/workflow。
- 只留一条总结评论（含 before→after finding 计数）。

### 写后验证

agent 推送后，workflow 重跑 `audit_design_docs.py --diff` 取 after 快照，对比 pre-sync 的 high finding 计数，写入 job summary。这把"文档同步是否真的改善了 audit"变成可量化证据，而非只看 agent 评论的自述。

## 边界

- 检测层只扫 `internal/`、API mounts、migrations、FE routes 四类 surface。`scripts/`、`web/` 非路由部分、配置文件不在扫描范围，不会自动报为 unmapped。
- surface_map 的完整性由人工 + agent 维护；audit 不验证"映射目标文档内容是否真的描述了该 surface"（只验证文件存在）。
- LLM 写入层不做业务代码改动，因此 audit 对 agent 引入的文档漂移只能事后发现，无法在 agent 运行中拦截业务逻辑变更。

## 兼容与测试

- `scripts/docs-audit/test_audit_design_docs.py` — audit 单元测试（提取、比对、accounting）。
- `design-doc-audit.yml` 在 PR CI 上验证检测层不退化（exit 2 硬失败）。
- 写入层 E2E：在私有 fork 上 `workflow_dispatch` 派发，验证 agent 能对未映射 surface 产出文档 + 更新 surface_map + 刷新 snapshot（历史验证 run：Postroggy/open-managed-agents PR #3，commit `docs: sync design docs for demo_widgets`）。

后续收敛方向：把 `design-doc-audit.yml` 的 exit 1 从软告警逐步收紧为硬失败；补 surface_map 中现存 `gated:needs-design-doc` 项。
