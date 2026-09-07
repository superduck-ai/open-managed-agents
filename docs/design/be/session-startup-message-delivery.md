# Session 启动期消息可靠投递

## 问题

Runner 过去在 prepare 阶段读取一次 `session_events` 快照，随后才创建 sandbox 和 Code
Session。快照之后、Code Session 创建之前发送的消息虽然已经写入 `session_events`，但不会
进入 runtime 消费的 `code_session_inbound_events`。

## 设计

`session_events` 是启动输入的唯一事实源，不增加临时 queue、watermark 或公开状态。

Send Events 与 Code Session activation 都先锁同一条 Session 行：

- Send 通过 `Service.SendPublicSessionEvents` 复用 `ManagedAgentEventTx`，先锁 Session，再锁最新 Code Session；active 时在同一事务内写公开事件与 inbound，其他状态先保存公开事件；
- activation 锁 Session 后读取完整公开历史，在同一事务中写 inbound 并将 Code Session 从
  `initializing` 切为 `active`。

因此只可能有两种顺序：

1. Send 先提交，activation 随后读取到该事件并写入 inbound；
2. activation 先提交，Send 随后读取到 active 状态，在自己的事务内保存公开事件并写入当前 batch 的 inbound。

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
        Send->>CS: lock latest Code Session (active)
        Send->>Events: INSERT current batch
        Send->>Inbound: append current batch
        Send->>Send: COMMIT
    end
```

## 激活流程

`Service.CreateManagedAgentCodeSession`：

1. 创建 `initializing` Code Session；
2. 写入 `initialize` inbound；
3. 调用 `ActivateManagedAgentCodeSession`；
4. activation 事务锁定 Session 和 initializing Code Session；
5. 按 `created_at ASC, id ASC` 读取完整 `session_events`；
6. 过滤并转换可转发事件，幂等写入 inbound；
7. 将 Code Session 切为 `active` 并提交。

任一历史转换、inbound 写入或状态更新失败时，activation 事务整体回滚，Code Session 保持
`initializing`。Deployment initial events 已经属于 `session_events`，无需单独交接路径。

## Realtime cutover

Send 的公开写入和 active worker inbound 入队使用同一个 Yourbatis executor。普通消息、工具确认和自定义工具结果先完成转换，再按输入顺序追加 inbound；工具待确认 metadata 的清理也在此事务内。任一转换、入队或清理失败时，公开事件、outcome、处理时钟、inbound 与序号一起回滚，HTTP 不会报告发送成功。

Send 和 activation 持有同一 Session 锁，交接检查不再发生在公开提交之后。现有 inbound idempotency key 仍负责历史回放和控制响应重试去重。activation 本身的历史读取顺序保持 `created_at ASC, id ASC`，本次未把启动回放协议改成公共列表的排序合同。

通知 broker 和恢复 sandbox 发生在事务提交之后。通知失败不改变已接受的事实，持久 inbound 仍可由现有 worker SSE/poll 读取。提交后的通知查询复用 activation 的 queued 读取；重复通知使用原消息身份。

## 验收

- Runner prepare 后、Code Session 创建前接受的消息最终进入 inbound；
- 启动期接受多条用户消息，activation 按公开历史顺序全部重放；
- activation 失败时不留下部分 inbound，也不切换为 active；
- activation 后的新 batch 只通过 realtime 路径追加；
- Deployment initial user messages 在 `initialize` 后按输入顺序进入 inbound。

- active Send 的 inbound 写入或工具 metadata 清理失败时，公开事件、outcome、处理时钟与队列一起回滚；
- 混合文本、工具确认、自定义工具结果的 batch 按原输入顺序进入 inbound；
- 重试成功后 Send 响应与历史 API 的完整事件 JSON 值一致。
