# Webhook 端点订阅断链修复（session.created / session.pending / session.archived）

> Issue: #255
> 日期: 2026-08-16
> 分支: `fix/webhook-session-lifecycle-events`

## 背景

OMA 的 webhook 系统在 session 生命周期事件上存在断链：`session.created`、`session.pending`、`session.archived` 三个事件**有产生方、有默认订阅，但端点订阅 API 与事件映射桥缺失**，导致：

1. 客户端通过 API 创建 webhook 端点订阅 `session.archived` → 直接 **400 "unsupported enabled_events value"**
2. ~~事件流路径（`webhookEventsFromSessionEvent`）不产生这三个事件~~（复查结论：这三个类型无 worker 侧产生者，映射桥路径不可达，此断链不成立；事件经直连 enqueue 送达）

## 官方契约对照

官方 Claude Managed Agents webhook 事件类型（`webhooks.md`）：
`session.status_run_started` / `session.status_idled` / `session.budget_reached` / `session.status_rescheduled` / `session.status_terminated` / `session.thread_created` / `session.thread_idled` / `session.thread_terminated` / `session.outcome_evaluation_ended` / `session.updated` / `session.deleted`

**结论**：`session.created` / `session.pending` / `session.archived` 是 OMA 扩展事件（官方没有 created/archived/pending 的 webhook 事件），但 OMA 已在 create/archive 时产生并默认订阅，属于**既有设计**，应保持并补全链路，而非移除。

## 现状

| 环节 | session.created | session.pending | session.archived | 证据 |
|---|---|---|---|---|
| 产生方 | ✅ create 时 enqueue | ✅ create 时 enqueue | ✅ archive 时 enqueue | `sessions/service.go:145-146,344`、`deployments/handler.go:648-649` |
| 默认订阅 | ✅ | ✅ | ✅ | `config/defaults.go:99-104` |
| 端点订阅允许列表 | ❌ | ❌ | ❌ | `webhooks/handler.go:33-56` |
| 事件映射桥 | ❌ | ❌ | ❌ | `sessions/webhook_bridge.go:68-108` |

## 方案

1. **`supportedEndpointEventTypes` 补全**（`internal/webhooks/handler.go`）：加入 `session.created`、`session.pending`、`session.archived`，与 `defaultWebhookEventTypes` 对齐。

> 注：三个生命周期事件的**产生方是 service/deployments 的直连 enqueue**（`sessions/service.go:145-146,344`、`deployments/handler.go:648-649`），**不经映射桥**，当前无 worker 侧产生者——因此映射桥不需要（也不应有）这三个 case。本 PR 只修白名单。
> 待办：`managedagentsevents.CategoryFor` 未包含三事件（落入 `CategoryUnknown`），当前无调用方受影响；如未来统一事件分类表，应归入 `CategorySessionStatus` 并评估对 `IsPersistedManagedAgentEvent` 等函数的影响。

## 测试

- `webhooks/handler.go` 的端点创建校验测试：`session.archived` 不再 400
- 回归：现有 22 个允许列表事件不受影响

## 验收

- 客户端 API 创建订阅 `session.archived` 的端点成功（不再 400）
- create / archive 时，订阅端点收到 `session.created` / `session.pending` / `session.archived` 事件
- 官方 11 个 webhook 事件仍全部可订阅
