# Transcript 上传与背压

运行 `just verify-be chat doctor replication`，再运行 `just verify-be chat replication`。Doctor 检查 Bun 与共同依赖，不要求 Worker 镜像、FUSE 或完整 Brain 二进制。

该场景由 Go CLI 启动独立后端、PostgreSQL、Redis、三节点 JetStream 与 MinIO，再由 Go 集成测试启动 checked-in Bun fixture。fixture 只负责驱动生产 `SessionReplication`、DSR client 和 内存 TranscriptPersistence；故障控制、数据库断言与 BE_PROOF 判定由 Go 完成，不使用临时编排脚本或生产故障开关。每个负载使用独立 run-owned Code Session 和进程内 transcript 记录。

| 必需阶段 | 证据 |
| --- | --- |
| `replication_batches` | 405 条小事件按 200/200/5 上传；6 条 1500 KiB 事件按 2/2/2 上传；代理计量实际 HTTP body ≤4 MiB，真实 PostgreSQL 逐条比较 seq、index 和 payload |
| `replication_backpressure` | 默认 32/16 MiB 水位；40 MiB 本地内存日志在真实 HTTP 请求被阻塞时包含在飞积压；容量等待保持，本地仍可追加一条 1 MiB 事件；放行后降至低水位且仍有尾部未上传时解除等待；最后核对 41 条远端正文 |
| `replication_413_terminal` | Go 代理只对 run-owned transcript 请求追加超出 16 MiB 的未知 envelope 字段，让真实后端返回 413；客户端只发送一次，不拆批、不重试；后续 flush 立即拒绝，本地保留两条、数据库水位为零 |
| `replication_shutdown_deadline` | 真实 HTTP 上传保持未响应，调用生产复制层默认 5 秒 drain；断言 4.5–8 秒内明确失败、连接被取消、等待中的 flush 拒绝、两条本地日志保留、数据库水位为零，关闭后无新上传 |

代理不返回模拟的 413；该状态由真实后端 body 限制产生。超时阶段只阻塞 run-owned HTTP，不修改共享服务、数据库时间或重试时钟。清理结束后才判定通过。

场景覆盖生产复制组件与真实后端的集成，不启动模型回合、完整 DsrOwner 或沙箱，因此不认证 owner 的 permanent release、进程级 SIGTERM、模型/工具准入和子 agent 公平性。后两者由普通测试覆盖，完整 DSR 执行另运行 `chat brain-dsr`；标准聊天冒烟另运行 `chat roundtrip`。不测量吞吐或性能基线，不认证新 transcript 归档和对象存储外置。
