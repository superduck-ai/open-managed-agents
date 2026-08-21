# 严格代码质量审查

## Summary

本改动实现了 E2B Sandbox 的 pause/resume 与“Provider 已删除后替换 Sandbox 并复用 Code Session”恢复链路：事件入队后 `SetTimeout` 唤醒，`ErrSandboxNotFound` 时原子失败旧 Sandbox 并重排队 Work，Runner 走 `RecoverManagedAgentCodeSession` 轮换凭证并注入递增 worker epoch。方向正确，测试也覆盖了临时失败重试与替换启动。

**不能批准。** 恢复路径被塞进原先只服务“新建 Code Session”的 Runner 失败清理流，导致恢复中途失败会 `Terminate` 本应保留的 durable Code Session——这是正确性 blocker。另外，`codesessions` 依赖 `e2bruntime` sentinel、用 Work metadata 开关控制 create/recover，以及把更多用例堆进已经 4900+ 行的 `sessions_api_test.go`，都是明显的结构债；存在可删除整类分支的 code-judo 空间。

## Issues

### Issue 1 -- Severity: bug
- File: internal/environments/runner.go:554
- Description: `createManagedAgentRuntimeLaunch` 在 recovery 分支调用 `RecoverManagedAgentCodeSession` 成功后，若 `buildEnvironmentManagerV0Payload` 失败，仍会执行 `TerminateManagedAgentCodeSession`。该清理逻辑原本假设“刚创建、可丢弃的 CSE”；恢复场景下 CSE 已承载持久化入站队列与会话身份。Terminate 会把本应复用的 Code Session 标为 terminated，入站事件随之失效。更糟的是后续 `failManagedAgentRuntime`（manager 启动失败 / publish metadata 失败，约 410–430 行）对 recovery 同样无条件 Terminate。恢复失败后 Work/Sandbox 也会被 fail/stop，自动恢复标记可能无法再次触发，会话会卡死。
- Suggestion: 把 launch 结果显式建模为 `created | recovered`（或 `PreserveCodeSession bool`）。仅 create 路径在失败时 Terminate；recover 路径失败时保留 CSE，并把 Work 重新标回 `queued` 且保留 `sandbox_recovery_code_session_id`（或等价恢复状态），以便 Runner 重试。补回归测试：Recover 成功后强制 payload/manager/publish 失败，断言 CSE 仍为 `active` 且队列消息仍在。
- Status: open

### Issue 2 -- Severity: bug
- File: internal/environments/runner.go:521
- Description: Create 与 Recover 共用同一套失败编排，是 Issue 1 的结构根因。Recovery 不是 Create 的特殊参数，而是另一种生命周期：身份保留、凭证轮换、失败可重试。现在用 `if RecoveryCodeSessionID != ""` 插入既有忙碌函数，再复用 `failManagedAgentRuntime`，属于往共享路径打补丁的 spaghetti 增长；行为上必然把“新建可销毁”与“恢复必须保留”混在一起。
- Suggestion: 拆出独立的 `recoverManagedAgentRuntimeLaunch`（或让 `CodeSessionRuntime` 暴露带明确失败语义的 API），Runner 主流程按模式分派；失败策略与 create 完全分离。目标是让 Terminate-on-failure 从恢复路径上消失，而不是再加一个 `if recovering` 分支。
- Status: open

### Issue 3 -- Severity: suggestion
- File: internal/codesessions/service.go:18
- Description: `codesessions.Service` 直接 import `internal/runtime/e2bruntime` 只为识别 `ErrSandboxNotFound`。这是资源编排层向具体 Provider 包的边界泄漏：换 Provider 或把 E2B 细节关在 runtime 后，codesessions 仍被钉死。也让 `resumeSandboxForCodeSession` 的错误语义依赖实现细节而非 `SandboxTimeoutExtender` 合同。
- Suggestion: 在 `codesessions`（或共享 provider 合同包）定义 `var ErrSandboxNotFound = errors.New(...)`，由 `e2bruntime.classifySandboxError` wrap 该 sentinel；或让 extender 返回可识别的领域错误类型。Service 不应依赖 `e2bruntime`。
- Status: open

### Issue 4 -- Severity: suggestion
- File: internal/environments/runner.go:849
- Description: 用 Work metadata 字段 `sandbox_recovery_code_session_id` 作为 create/recover 控制平面，再在 `publishManagedAgentRuntime` 里用 `nil` patch“清除”。这是临时旗标式设计：解析 helper、SQL jsonb_build_object、publish 时写 null、Go 侧依赖 null→空字符串，概念分散在 DB SQL、Runner preparation、publish 三处。PostgreSQL `||` 合并 null 只会把键写成 JSON null 而非删除，留下脏 metadata。
- Suggestion: code-judo——把恢复意图做成显式状态，而不是 metadata 副作用。例如 Work `data`/`state` 旁路字段、或 `environment_work` 上的 typed recovery marker，最好与 `ScheduleRecoveryForCodeSession` 同事务写入并在成功绑定 runtime 时清除。Runner 读强类型字段而不是 JSON 侦察。清除用 SQL `metadata - 'key'` 或专用 update，不要依赖 null merge。
- Status: open

### Issue 5 -- Severity: suggestion
- File: internal/codesessions/managed_agent_code_session.go:137
- Description: `RecoverManagedAgentCodeSession` 与 Create 尾部重复：发 OAuth token、读 credential context、签 ingress JWT、拼 `ManagedAgentCreateResult`（含 `CurrentWorkerEpoch + 1`）。Recover 还多一次 `GetCodeSession` round-trip。Rotate 未 bump epoch，只清 lease；在 Register 完成前，旧 epoch 仍能通过 `ValidateCodeSessionWorkerEpoch`，旧 session-ingress JWT 仍有效，存在恢复窗口内的 fencing 空隙（heartbeat 因 lease=null 会被拒，但事件写入路径主要看 epoch）。
- Suggestion: 抽取 `issueSandboxCredentials(ctx, org, ws, codeSessionID) (token, ingress, epochHint, error)` 供 Create/Recover 共用。认真考虑在 `RotateCredentials` 时立即 bump/作废旧 epoch（并相应调整 Register 与 env 注入合同），让恢复一开始就 fence 旧 worker，而不是等 Register。
- Status: open

### Issue 6 -- Severity: suggestion
- File: tests/sessions_api_test.go:769
- Description: 该文件已约 4904 行，本改动再加约 168 行。审查标准明确反对无必要地继续膨胀超大文件。新增的 resume/recovery E2E 有价值，但不该继续堆进巨型 suite。
- Suggestion: 抽到聚焦文件，例如 `tests/sessions_sandbox_recovery_test.go`（或 `tests/sandbox_lifecycle_api_test.go`），共享 `newTestApp` fixture。同步为 recover 失败不 Terminate 补测试（见 Issue 1）。
- Status: open

### Issue 7 -- Severity: nit
- File: internal/environments/runner.go:585
- Description: `patchJSONMetadata(..., {"sandbox_recovery_code_session_id": nil})` 依赖 JSON null 语义清除控制旗标，可读性差，且留下 null 键。
- Suggestion: 若短期仍用 metadata，提供 `deleteJSONMetadataKeys` 或 DB 侧 `metadata - 'sandbox_recovery_code_session_id'`；与 Issue 4 的显式状态方案一并处理更佳。
- Status: open
