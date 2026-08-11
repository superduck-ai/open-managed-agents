# Session 启动期消息可靠投递

## 问题

Runner 过去在 prepare 阶段读取一次 `session_events` 快照，随后才创建 sandbox 和 Code
Session。快照之后、Code Session 创建之前发送的消息虽然已经写入 `session_events`，但不会
进入 runtime 消费的 `code_session_inbound_events`。

## 设计

`session_events.payload` 是启动输入的唯一公开事实源，不增加临时 queue、watermark、第二份
Session Event payload 或公开状态。Files API 引用只在生成 Code Session inbound 时转换为
Claude Code 可读取的挂载路径：启动历史在 activation 转换，active Session 的新事件在 realtime
入口转换，详见
[Session 事件文件引用](./session-event-file-references.md)。

Send Events 与 Code Session activation 都先锁同一条 Session 行：

- Send 通过 `DB.WithSessionEventWriteTx` 锁 Session，在同一事务中解析活动文件绑定并提交公开事件；
- activation 锁 Session 后读取完整公开历史，在同一事务中写 inbound 并将 Code Session 从
  `initializing` 切为 `active`。

因此只可能有两种顺序：

1. Send 先提交，activation 随后读取到该事件并写入 inbound；
2. activation 先提交，Send 随后由现有 active realtime 路径投递当前 batch。

```mermaid
sequenceDiagram
    participant Client
    participant Send as Send transaction
    participant Session as sessions row
    participant Events as session_events
    participant Activate as Activation transaction
    participant CS as code_sessions
    participant Inbound as code_session_inbound_events

    alt Send 先锁 Session
        Send->>Session: SELECT FOR UPDATE
        Activate->>Session: wait
        Send->>Events: INSERT current batch
        Send->>Send: COMMIT
        Activate->>Session: acquire lock
        Activate->>Events: read complete history
        Activate->>Inbound: append forwardable events
        Activate->>CS: initializing → active
        Activate->>Activate: COMMIT
    else Activate 先锁 Session
        Activate->>Session: SELECT FOR UPDATE
        Send->>Session: wait
        Activate->>Events: read complete history
        Activate->>Inbound: append forwardable events
        Activate->>CS: initializing → active
        Activate->>Activate: COMMIT
        Send->>Session: acquire lock
        Send->>Events: INSERT current batch
        Send->>Inbound: active realtime delivery
    end
```

## 激活流程

`Service.CreateManagedAgentCodeSession`：

1. 创建 `initializing` Code Session；
2. 写入 `initialize` inbound；
3. 调用 `ActivateManagedAgentCodeSession`；
4. activation 事务锁定 Session 和 initializing Code Session；
5. 按 `created_at ASC, id ASC` 读取完整 `session_events`；
6. 读取当前活动 File Resource，过滤公开历史并通过 `workerPayloadForPublicEvent` 生成 worker 专用 payload；
7. 幂等写入 inbound；
8. 将 Code Session 切为 `active` 并提交。

任一历史转换、inbound 写入或状态更新失败时，activation 事务整体回滚，Code Session 保持
`initializing`。Deployment initial events 已经属于 `session_events`，无需单独交接路径。

## Realtime cutover

Send 提交公开事件后始终调用 `Service.QueuePublicSessionEvents`。该方法重新读取最新 Code
Session，只有 `status == active` 时才通过 `workerPayloadForPublicEvent` 写 inbound；不存在或仍为
`initializing` 时直接返回。文件转换使用 Send 事务内已经校验过的绑定快照，不在提交后重新查询
Resource，从而避免已接受事件与 realtime 转换之间的删除竞态。被公开事件引用的 File Resource
不能在 Session 生命周期内单独删除，activation 仍可从公开历史和活动挂载重建相同 worker payload。

如果 activation 恰好在公开事件提交后、realtime 检查前完成，同一事件可能同时出现在
activation 历史和 realtime 尝试中；现有 inbound idempotency key 会保留一份，不会重复投递。

## 验收

- Runner prepare 后、Code Session 创建前接受的消息最终进入 inbound；
- 启动期接受多条用户消息，activation 按公开历史顺序全部重放；
- activation 失败时不留下部分 inbound，也不切换为 active；
- activation 后的新 batch 只通过 realtime 路径追加；
- Deployment initial user messages 在 `initialize` 后按输入顺序进入 inbound。
