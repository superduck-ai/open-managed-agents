# Outcomes 链路修复与 define_outcome 契约对齐

> 当前完整架构方案见：[Managed Agent Outcome Grader 架构设计](./managed-agent-outcome-grader-architecture.md)。本文保留 API 局部实现和证据边界，不再作为最终架构选型文档。

> Issue: #246
> 日期: 2026-08-16（修订 2026-09-16）
> 分支: `fix/outcomes-link-and-grader`
> 本文记录 API 字段校验/存储的局部实现，不代表 outcome 执行闭环完成。2026-09-16 修正了此前对 EM 修复范围、资源状态与 webhook 时机的过强表述；选型及证据链见 [运行时复用复评](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md)。

## 背景

官方 [Claude Managed Agents 的 Outcomes 能力](https://platform.claude.com/docs/en/managed-agents/define-outcomes)：发送 `user.define_outcome`（`description` + `rubric` + `max_iterations`），harness 自动 provision 一个 grader（独立上下文窗口）按 rubric 评估产物，反馈给 agent 迭代。

OMA 此前**只有半个协议实现**：能接收 `user.define_outcome`、生成 `outc_` id、校验 `max_iterations`，但不解析 `description`/`rubric`，且存在 3 个链路 bug。

本 PR 范围：**OMA 侧 API 契约对齐**（字段校验、完整存储、透传数据就绪）。grader 的运行时接入、运行中目标消费及状态闭环为后续工作（见「范围边界」）；OMA 不自写模型/tool loop，但仍需 outcome 控制状态、评分触发和反馈。

当前总体设计已经确定为一个 managed session 对应一个 sandbox，EM 管理工作 agent A 与 grader G，各有独立上下文、复用现有 Claude agent loop；实现顺序是先完成 EM 的 1:N execution，再接 OMA outcome 事件驱动。该能力尚未实现；workflow 不是产品合同前提，hidden workflow 研究仅作机制记录。完整方案见 [Managed Agent Outcome Grader 架构设计](./managed-agent-outcome-grader-architecture.md)。

## 官方契约与本分支记录

- `user.define_outcome`：`description`（必填）、`rubric`（必填，`{type:text, content}` 或 `{type:file, file_id}`，无第三种）、`max_iterations`（可选，默认 3，上限 20）
- 官方要求提交 outcome 时携带 rubric（inline text 或文件引用）。这不排除先用 Claude 辅助编写 rubric；不能把“字段必填”扩大成“无法自动生成 rubric”
- 官方说明定义事件会 echo，并含 `processed_at` / `outcome_id`；本分支同时补齐事件 `id` / `created_at`
- 本分支 `outcome_evaluations` 数组每次保存定义增加一条记录；这只是当前记录方式，不代表已实现官方单活动 outcome 与完整资源 `result` 状态合同

## 数据流

```
用户发送 user.define_outcome {description, rubric, max_iterations}
  │
  ▼
① normalizeInputEvent（协议边界）
   ├─ 整数入口约束：max_iterations 必须是整数（3.5 拒绝）
   ├─ validateDefineOutcomePayload：description 必填 / rubric 结构 / 范围 1-20
   ├─ file rubric 存在性校验（validateRubricFile 回调 → GetFile）
   ├─ 生成 outcome_id / id / processed_at / created_at（echo 契约）
   └─ appendOutcomeEvaluation：存完整 description + rubric（不只摘要）
  │
  ▼
② 持久化
   ├─ SessionEvent.Payload：完整事件（含 description/rubric）
   └─ Session.OutcomeEvaluations：evaluation 记录（含完整目标定义）
  │
  ▼
③ 构造 environment-manager 启动配置（不是运行中目标投递的证明）
   └─ sessionConfig.outcomes：真实数组（非硬编码空），含完整 description/rubric
      ⚠️ Rust EM OutcomeField / parser 当前是 Git 仓库结果模型，不消费 rubric 评分
      ⚠️ 运行中的 user.define_outcome 还被 OMA worker 输入白名单过滤
      （见「范围边界」；不是仅扩展一个 type 就能完成 grader）
```

## 实现设计

### 1. 协议边界：`normalizeInputEvent`

**整数入口约束**（`requireIntegerPayloadField`）：
- JSON 数字经 `json.Unmarshal` 解码为 `float64`，`max_iterations: 3.5` 会成功断言成 `float64(3.5)`
- 在入口用 `math.Trunc(value) != value` 拒绝非整数，**业务层无需浮点兜底**
- 通用 helper（字段名参数化），其他整数语义字段可复用
- 位置：`normalizeInputEvent` 紧挨解码处（`eventType == "user.define_outcome"` 时）

**rubric 校验**（`validateDefineOutcomePayload`，纯函数）：
- `description`：必填，非空字符串
- `rubric`：必填对象，`type` 只能是 `text`（`content` 非空）或 `file`（`file_id` 非空）
- `max_iterations`：范围 1-20（整数性已由入口锁死）

**file rubric 存在性校验**（`validateRubricFile` 回调）：
- 设计：`normalizeInputEvent` 是纯函数（无 handler/db），通过**回调参数**注入文件校验，保持可测
- 调用方（`service.go`）注入：`GetFile` 查存在，`ErrNotFound` → `resourceNotFound("rubric file", err)`（走 errors.go 命名构造）
- 回调内 `file_id` 严格断言为**非空字符串**（`file_id: 123` 这类断言失败直接报错，不留逃生口）
- 语义与 Files API 可见性一致（file rubric 是用户上传的共享 rubric）

### 2. 完整存储：`appendOutcomeEvaluation`

- 旧行为：只存摘要 `{id, outcome_id, max_iterations, status, type, updated_at}`
- 新行为：**完整透传 `description` + `rubric`**——evaluation 记录携带评分标准，供后续 grader 消费
- `description` 非字符串静默跳过（防御）；`rubric` 为 nil 不写

### 3. 透传：`environment_manager.go`

- `rawJSONArrayOrEmpty`：从 `session.OutcomeEvaluations` 解码，空/null/无效时返回空数组（非 nil，payload 稳定）
- 测试钉住：sessionConfig.outcomes 数组必须含 `description` + `rubric.type/content`（透传断言）

### 4. deployment 路径对齐

- `deploymentOutcomeEvaluation` 增加 `Description` / `Rubric` 字段（此前只存摘要）
- `sessionEventsFromInitialEvents` 构建时透传（`deploymentOutcomeRubricRaw` marshal）
- 与 sessions 主路径一致：deployment 创建的 session 到沙箱时 outcome 也携带完整目标定义

### 5. 分类注册

- `session.outcome_evaluation_ended` 注册进 `CategoryFor` → `CategorySessionStatus`。这只说明分类器认识该名称，不证明评估真的完成或 webhook 触发时机正确

## 范围边界（以当前源码为准）

源码基线：OMA H `e43888edc0f7dff91616896f9cc3714a280fa37b`；Rust EM R `544e59ed9e3c7c862d0b8c1a70c58e6c6c18d890`。这部分是静态源码事实，不是 E2E 结果。

- **启动数据与运行中投递分开**：[sessionConfig.outcomes](/Users/yueqi/Coding/Agent/open-managed-agents/internal/environments/environment_manager.go#L55)已携带目标定义；但运行中事件经 [QueuePublicSessionEvents](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/service.go#L53)和[输入白名单](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/service.go#L591)过滤，`user.define_outcome` 不会由这条路径投递给 worker。不能把启动快照描述成已接通动态执行。
- **Rust EM 的同名 outcomes 是不同模型**：[OutcomeField](/Users/yueqi/Coding/Agent/environment-manager-rs/src/internal/config/types.rs#L224)只有 type/git_info；[V0 parser](/Users/yueqi/Coding/Agent/environment-manager-rs/src/internal/input/v0_parser.rs#L183)处理 Git 仓库/分支。扩展类型判别并不自动产生独立评分上下文、工具调用、verdict、反馈或中断能力。
- **grader 执行尚未接入**：Claude 原生多上下文、确定性 skill 和单步 workflow 的证据保留，但不据此优先选择 hidden workflow。同 sandbox 双进程候选仍需独立历史/resume、权限、私有结果和用量分流；普通 [result→session.status_idle](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/mapper.go#L446) 不能直接接收 grader 结果，[权限仍按 owning session 主 agent snapshot](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/tool_permissions.go#L88) 解析。不是“另注册一个完整 worker/lease”和“OMA 重写 loop”的二选一。
- **sandbox 与 EM 装配边界**：OMA [provider.Create](/Users/yueqi/Coding/Agent/open-managed-agents/internal/environments/runner.go#L259) 创建 sandbox，[runner](/Users/yueqi/Coding/Agent/open-managed-agents/internal/environments/runner.go#L330) 在其中启动 EM。R 当前只有单 executor 装配，尚无一 session 管理多个独立 Claude 进程的能力；这不是 OS/sandbox 限制，one executor 不等于 one context。共享 run token/session/token 文件的证据见 [主复评 3.2](/Users/yueqi/Coding/Agent/open-managed-agents/docs/design/be/outcomes-grader-em-review.md)。
- **评分 span 有分类，没有已接通的评分闭环**：[CategoryFor](/Users/yueqi/Coding/Agent/open-managed-agents/internal/managedagentsevents/events.go#L37)识别 `span.outcome_evaluation_*`；分类/透传不等于真实评估产生方。
- **资源状态尚未对齐**：[appendOutcomeEvaluation](/Users/yueqi/Coding/Agent/open-managed-agents/internal/sessions/service_helpers.go#L472)写的是 `status: pending`，不是已实现官方 `result`。`running/evaluating/终态`、轮次和结果字段需要与实际评估状态同步设计。
- **webhook 时机尚未对齐**：[sendEvents](/Users/yueqi/Coding/Agent/open-managed-agents/internal/sessions/service.go#L586)在定义发生变化时发出 `session.outcome_evaluation_ended`；[官方 Webhooks](https://platform.claude.com/docs/en/managed-agents/webhooks)定义的是单次迭代评估完成，不能将目前行为列为通过验收。
- **完成与中断尚未闭环**：worker 的 [requires_action 和 idle 被合并](/Users/yueqi/Coding/Agent/open-managed-agents/internal/codesessions/status.go#L27)，不能仅据 idle 触发评分；interrupt→interrupted、取消和迟到结果处理都仍待实现与真实验证。

## 测试

以下是本分支已有的局部测试点；2026-09-16 本次仅修订文档，未重新执行这些测试，且它们不证明真实 grader 已运行。

- 整数入口：`3.5` 被拒（`TestNormalizeInputEventRejectsFractionalMaxIterations`）
- rubric 校验：text/file 结构 + 缺失字段报错（失败场景先行）
- file 存在性：文件缺失报错 / 存在通过 / text 不触发（`TestNormalizeInputEventValidatesRubricFile`）
- file_id 类型：非字符串报错（`TestNormalizeInputEventRejectsNonStringRubricFileID`）
- 透传：environment_manager sessionConfig 含完整 description + rubric（`TestManagedAgentSessionConfigCarriesOutcomeEvaluations`）
- deployment：`prepareDeploymentExecution` 的 OutcomeEvaluations 携带 description/rubric（`TestPrepareDeploymentExecutionCarriesOutcomeDefinition`）
- 分类：`CategoryFor("session.outcome_evaluation_ended")` 返回 CategorySessionStatus

## 局部验收与未完成项

- 局部验收目标：description/rubric/max_iterations 校验与 echo；不将其表述为完整 outcome 契约已对齐
- evaluation 记录与下发数组携带完整目标定义（description + rubric）
- deployment 与 sessions 两路径一致
- 分类器认识 `session.outcome_evaluation_ended`；真实评估完成与正确 webhook 发送时机不在已完成项中
- rubric 定义已保存；运行中投递、独立上下文、真实评分反馈、状态/事件一致性、中断和恢复仍未闭环
