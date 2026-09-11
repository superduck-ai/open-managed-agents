# Outcomes 链路修复与 define_outcome 契约对齐

> Issue: #246
> 日期: 2026-08-16（修订 2026-08-27）
> 分支: `fix/outcomes-link-and-grader`

## 背景

官方 Claude Managed Agents 的 Outcomes 能力：发送 `user.define_outcome`（`description` + `rubric` + `max_iterations`），harness 自动 provision 一个 grader（独立上下文窗口）按 rubric 评估产物，反馈给 agent 迭代。

OMA 此前**只有半个协议实现**：能接收 `user.define_outcome`、生成 `outc_` id、校验 `max_iterations`，但不解析 `description`/`rubric`，且存在 3 个链路 bug。

本 PR 范围：**OMA 侧 API 契约对齐**（字段校验、完整存储、透传数据就绪）。grader 引擎与 environment-manager 消费为后续工作（见「范围边界」）。

## 官方契约（对齐基准）

- `user.define_outcome`：`description`（必填）、`rubric`（必填，`{type:text, content}` 或 `{type:file, file_id}`，无第三种）、`max_iterations`（可选，默认 3，上限 20）
- rubric 必须由用户提供（inline text 或 Files API 上传），无 agent 自动生成机制
- 事件 echo：`id` / `outcome_id` / `processed_at` / `created_at`
- `outcome_evaluations` 数组：每 define_outcome 一条记录

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
③ 下发 environment-manager
   └─ sessionConfig.outcomes：真实数组（非硬编码空），含完整 description/rubric
      ⚠️ EM 侧 v0_parser.rs:189 只认 type=="git_repository"，当前静默丢弃
      （见「范围边界」）
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

- `session.outcome_evaluation_ended` 注册进 `CategoryFor` → `CategorySessionStatus`，事件可持久化/进历史

## 范围边界（如实声明）

- **environment-manager 消费**：`sessionConfig.outcomes` 已携带完整 evaluation 数据，但 EM 侧（`environment-manager-rs`）`v0_parser.rs:189` 只消费 `type=="git_repository"` 的 outcome，evaluation 数据当前被静默丢弃。需 EM 侧适配（type 判别扩展）后才真正到达 agent
- **grader 引擎**：未实现（需模型调用 + 架构决策：沙箱独立 worker vs OMA 侧 go 服务）。依赖本 PR 的完整存储（grader 需要 rubric 数据）
- **`span.outcome_evaluation_*` 事件**：分类已存在（worker 发了即透传），无产生方（依赖 grader）
- **`outcome_evaluations[].result` 多态**：当前只有 `pending`，`running/evaluating/终态` 需 grader
- **interrupt → interrupted**：未实现（依赖 grader 的状态机）

## 测试

- 整数入口：`3.5` 被拒（`TestNormalizeInputEventRejectsFractionalMaxIterations`）
- rubric 校验：text/file 结构 + 缺失字段报错（失败场景先行）
- file 存在性：文件缺失报错 / 存在通过 / text 不触发（`TestNormalizeInputEventValidatesRubricFile`）
- file_id 类型：非字符串报错（`TestNormalizeInputEventRejectsNonStringRubricFileID`）
- 透传：environment_manager sessionConfig 含完整 description + rubric（`TestManagedAgentSessionConfigCarriesOutcomeEvaluations`）
- deployment：`prepareDeploymentExecution` 的 OutcomeEvaluations 携带 description/rubric（`TestPrepareDeploymentExecutionCarriesOutcomeDefinition`）
- 分类：`CategoryFor("session.outcome_evaluation_ended")` 返回 CategorySessionStatus

## 验收

- `user.define_outcome` 全字段契约对齐（description/rubric/max_iterations 校验 + echo）
- evaluation 记录与下发数组携带完整目标定义（description + rubric）
- deployment 与 sessions 两路径一致
- `session.outcome_evaluation_ended` 进入公开历史
- grader 引擎落地时，rubric 数据已就绪（本 PR 的地基）
