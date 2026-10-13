# 后端验证功能地图

按用户可观察到的问题选择场景。
每个场景都必须完成全部断言和清理。
单个阶段成功不足以判定整个场景通过。

| 用户问题                                 | 功能说明                       | 验证命令或范围                                               |
| ---------------------------------------- | ------------------------------ | ------------------------------------------------------------ |
| 发送消息后没有回复                       | [消息收发](roundtrip.md)       | `chat roundtrip`                                             |
| 上游鉴权失败或重试中断后，回合未正确结束 | [上游错误](upstream-errors.md) | `chat upstream-errors`                                       |
| 重新打开会话后回复丢失                   | [历史恢复](history.md)         | `chat.roundtrip` 检查成功回合。`chat.reliability` 检查流中断 |
| 工具允许或拒绝结果错误                   | [工具审批](tools.md)           | `chat tools`，使用官方 Go SDK                                |
| 忙时拒绝、空闲重试或工作进程替换异常     | [连接恢复](recovery.md)        | `chat reliability`                                           |
| 另一个 API 实例无法订阅回复              | [跨实例交付](recovery.md)      | `chat instances`                                             |
| 公开接口创建会话后启动失败               | [会话启动](public-start.md)    | `chat public`，使用真实后台执行器和本地 Docker 沙箱          |
| 子智能体不可用、使用父权限或丢失线程     | [子智能体委派](subagents.md)   | `chat subagents`，检查技能、MCP、审批和原线程续聊            |
| 聊天延迟退化                             | [性能比较](performance.md)     | `chat performance --baseline REPORT`                         |

本地聊天场景使用脚本提供的模型响应。
这些场景不验证云端分配、云端故障自动检测、浏览器展示或真实模型质量。
固定负载测试不代表并发容量验证。
云端适配器场景单独检查服务商操作。

## 文件

阅读[文件验证说明](files.md)。
先运行 `just verify-be files doctor`。
再运行九个本地功能场景。
按[文件性能说明](files-performance.md)运行基线与候选版本比较。

`files generated` 需要工作进程镜像和 FUSE。
先运行 `files doctor generated` 检查该场景的依赖。
[云端适配器验证](cloud.md)需要显式提供专用凭据。
云端场景独立于本地场景。

## 回复记录归档

阅读[归档、恢复和完整性说明](transcript.md)。
先运行 `just verify-be transcript doctor`。
再运行 `integrity`、`recovery`、`concurrency`、`boundary` 和 `lifecycle`。

这些集成场景使用真实 PostgreSQL 和 MinIO。
`lifecycle` 还调用维护命令。
这些场景不需要工作进程，也不验证 River 后台调度。

## 记忆与文件存储

阅读[记忆与文件存储说明](memory.md)。
先运行 `just verify-be memory doctor`。
再运行 `integrity`、`isolation`、`cleanup`、`lifecycle` 和 `filestore`。
这些场景使用生产 HTTP 处理器及真实 PostgreSQL 和 MinIO。

实际挂载另行验证。
先运行 `memory doctor mounts`，再运行 `memory mounts`。
该场景检查真实后台执行器、Docker、FUSE 和跨会话读取。

## 部署与 River

阅读[部署验证说明](deployment.md)。
先运行 `just verify-be deployment doctor`。
再运行 `lifecycle`、`retry`、`idempotency` 和 `restart`。

这些场景检查生产接口、River 重试、终态、事务副作用和重复执行。
`restart` 检查 SIGKILL 后的进程恢复，并使用测试专用恢复期限。
会话模型回合需要单独验证。
