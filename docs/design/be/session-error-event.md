# session.error 事件（类型化错误对象）

> Issue: #251
> 日期: 2026-08-16
> 分支: `feat/session-error-event`

## 背景

官方 Claude Managed Agents 的事件类型表里有 `session.error` 事件：session 处理出错时，平台发一个类型化错误对象，含 `type`（错误类型）和 `retry_status`（是否自动重试）。这是客户端**观察错误的主通道**——流式示例都按 `event.error.message` 读取。

OMA 当前**不产生 `session.error`**：`managedagentsevents/events.go:32` 只有分类，无构造点；worker 错误被映射为 `session.thread_status_terminated`（``systemPublicPayloadCandidates` 的 task_notification 分支`），客户端只能靠「线程终止」推断出错。

## 官方契约

- `session.error` 事件：类型化 error 对象
- 字段：`type`（错误类型）、`retry_status`（是否自动重试）、`message`
- 触发时机：session 处理出错时（含 MCP 连接失败、模型错误、内部错误等）

## 现状

- `managedagentsevents/events.go:32`：`session.error` 仅分类（CategorySessionStatus），无产生
- `codesessions/`systemPublicPayloadCandidates` 的 task_notification 分支`：`task_notification` 的 status 为 `failed`/`error`/`terminated` → 映射为 `session.thread_status_terminated`
- `codesessions/status.go:27-36`：`publicEventTypeFromWorkerStatus` 只映射 running/idle/requires_action（worker 只上报这三种）
- 无 `retry_status` 概念

## 方案

1. **`CategoryFor` 已有 `session.error` 分类**（无需改）
2. **`mapper.go` 的 `task_notification` 分支**：status 为 `failed`/`error`/`terminated` 时，**同时产生 `session.error` 事件**：
   - `type`：`schema.Status` 小写化派生（`failed` / `error` / `terminated`）
   - `retry_status`：`"not_retryable"`（worker 上报 failed/error 表示不可自动重试；可重试错误走 rescheduled）
   - `message`：`schema.Summary`（task_notification 已带 summary）
   - `session_thread_id` / `task_id` / `tool_use_id`：与 thread_status_terminated 同源
3. **事件顺序**：`session.error` 在 `session.thread_status_terminated` 之前发出（官方「先 error 后终止」语义）

## 测试

- `mapper.go`：task_notification 的 status=failed/error/terminated → 产生 session.error（type/retry_status/message 正确）+ thread_status_terminated
- status=completed/succeeded → 不产生 session.error
- `CategoryFor("session.error")` 回归

## 验收

- worker 上报 failed/error 的 task_notification 时，事件流出现 `session.error`（带 type/retry_status/message）
- 可重试错误（rescheduled）不产生 error 事件（走 rescheduling 语义）
- 官方 SDK 的 `session.error` 处理分支可正常工作
