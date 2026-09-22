# Qoder Cloud Agents 公开事件实测

验证日期：2026-09-21。参考 Session：
[sess_00q4pbgl1io74ctu26vw](https://qoder.com.cn/cloud/sessions/sess_00q4pbgl1io74ctu26vw)。
本记录区分实际观察、官方合同和对本项目的设计建议；不推断 Qoder 的内部实现。

## 验证方法与结果

使用中国区官方 API `https://api.qoder.com.cn/api/v1/cloud`，Bearer 鉴权。
密钥不写入文件。先 GET Session、事件和线程：初始事件列表为空，只有一个 idle 主线程。
随后订阅 SSE，再通过 POST events 发送两条测试消息：只回复“你好”；执行 `printf probe-ok` 后回复。
第二条触发工具审批，读取并确认实际命令确为 `printf probe-ok` 后，仅批准该工具调用。
没有读取或修改项目文件。测试结束后 Session 为 idle/end_turn，无待处理审批。

捕获 38 个 SSE 帧，包含 31 条持久事件和 7 个预览帧；刷新后的历史共 31 条。
剔除 event_start/event_delta 后，SSE 与历史的事件 ID 序列完全相同。
三个 model request start 都有对应 end，五个 event_start 的 ID 均匹配最终持久消息。
两种测试的公开事件流中 system.message、初始化信息、Hook 通知、原始 result 均未出现。
这是 API 层的实测结果，不是根据网页隐藏行为推断，也不保证所有错误场景都不会出现 system.message。

## 普通回答的实际顺序

下列为持久事件顺序；预览帧穿插在对应消息前，不进入历史：

```text
session.status_running
session.thread_status_running
user.message
span.model_request_start
agent.thinking
agent.message
span.model_request_end
session.thread_status_idle   stop_reason=end_turn
session.usage
session.status_idle          stop_reason=end_turn
```

前三条的 processed_at 完全相同。回答、end、thread idle、usage、session idle 也共享一个时间戳。
按 `(processed_at, id)` 重排这些事件，会改变服务器实际返回的顺序；ID 字典序不能代替业务顺序。
这不证明 Qoder 使用了哪种内部序列号，只证明消费者需要保留其提供的稳定顺序。

## 工具审批与继续执行

```text
模型请求 A：start → thinking → agent.tool_use → end
thread idle → session.usage → session idle
  stop_reason={type:requires_action,event_ids:[工具事件 ID]}
user.tool_confirmation       tool_use_id=上述工具事件 ID
session running → thread running
agent.tool_result            tool_use_id=同一工具事件 ID
模型请求 B：start → thinking → agent.message → end
thread idle → session.usage → session idle
  stop_reason={type:end_turn}
```

请求 A、B 的 end 分别引用自己的 start，不把审批等待和工具执行算成一次长推理。
idle 表示等待或本轮结束，须结合 stop_reason；不能把所有 idle 都当作成功完成或重复事件。

| 请求 | start 的 processed_at（UTC） | end 的 processed_at（UTC） | 间隔 |
| --- | --- | --- | --- |
| 普通回答 | 15:17:49.188169337 | 15:17:51.631154 | 2442.985 ms |
| 决定调用工具 | 15:18:36.622730049 | 15:18:39.585244 | 2962.514 ms |
| 工具执行后回答 | 15:19:47.079140456 | 15:19:49.733619 | 2654.479 ms |

## 关联字段与官方合同

- end.model_request_start_id 引用 start.id；实测 end 还有 is_error 和 model_usage.credits。
- 实测 agent.message 不带 model_request_start_id，end 也没有本项目扩展的 event_ids/tool_use_ids。
  不能宣称 Qoder 对每条消息都提供了显式请求归属，或据此删除本项目针对异步路径的关联字段。
- event_start.event.id、event_delta.event_id、最终 agent.message.id 相同；thinking 只有 start 预览，无文本 delta。
- 主线程也有 thread running/idle；它们携带 session_thread_id，与 Session 状态是不同层级。
- system.message 是正式文本消息类型，允许客户端在 user/tool result 之后提交，并非通用日志或错误信封。
- 正式错误事件为 session.error；重试语义由 error.retry_status 表达，最终状态以后续状态事件为准。
- 无 Last-Event-ID 的新 SSE 连接只收到连接后的事件；完整历史使用 List Events。
- 本次未验证并发子线程、网络重试或错误/取消。不能由公开流判断 Qoder 内部由 Worker 状态还是 result 驱动收尾。

官方来源：
[认证](https://docs.qoder.cn/cloud-agents/api-authentication)、
[列出事件](https://docs.qoder.cn/cloud-agents/api/sessions/events/list)、
[发送事件](https://docs.qoder.cn/cloud-agents/api/sessions/events/send)、
[事件流](https://docs.qoder.cn/cloud-agents/api/sessions/events/stream)、
[事件结构](https://docs.qoder.cn/cloud-agents/session-schemas)、
[多线程可见性](https://docs.qoder.cn/cloud-agents/multi-agents)、
[SSE 新连接语义](https://docs.qoder.cn/cloud-agents/sse-initial-connection-behavior-change)。

## 对本项目的设计建议

1. 在后端公共事件映射处区分业务事件与运行时诊断。普通 init、Hook 和成功 result 不应默认包装为 system.message。
   原始诊断的保存应使用内部记录路径，先核实该路径覆盖范围；不能靠公开事件兜底而无意泄露内部信息。
2. 保留具有真实公开消息语义的 system.message；不能仅凭该类型在前端全部隐藏。
   执行错误保留明确的公开错误信号，不能与成功诊断一并丢弃。
3. 保留真实模型请求边界和主/子线程关联；Session/Thread 状态与模型请求生命周期分开，idle 保留准确原因。
4. 实时和历史使用相同的服务端稳定事件顺序；时间戳用于时间条几何，不用随机 ID 猜测相同时间戳事件的因果顺序。
   具体排序实现应单独收敛范围，避免再次顺带改变时间过滤字段或分页合同。

后续实现已按上述观察收敛，具体行为及与 Qoder 的差异见 [Worker 事件设计](ccr-v2-worker-sse-fanout.md#每次模型请求的生命周期)。
