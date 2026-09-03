# NATS 消息基础设施

## 当前边界

应用在组装层创建一条全局 NATS 连接，确认目标账号已启用 JetStream，并在进程退出时 drain。Session SSE 的实时 fanout 固定使用这条连接上的 Core NATS Pub/Sub，不再提供 Redis fanout。River job、最终事件的 PostgreSQL 持久化和 Redis 平台登录会话保持不变。后续 producer 和 consumer 应复用这条连接，不得在 handler 或每次请求内重新连接。

```mermaid
flowchart LR
    yaml["nats YAML"] --> loader["config.Load"]
    loader --> main["main assembly"]
    main --> client["natsclient.Open"]
    client --> core["NATS connection"]
    client --> js["JetStream readiness check"]
    core --> fanout["Core NATS session fanout"]
    fanout --> hub["instance Hub → SSE"]
    main --> drain["graceful drain on shutdown"]
```

`nats.url` 是部署必填项，可使用逗号分隔多个种子 URL；`nats.enabled` 已从配置合同移除。服务采用 fail-closed 启动：所有种子都无法连接或 JetStream 不可用时，HTTP server 不会开始监听，也不会回退到 Redis。默认连接超时为 5 秒，排空超时为 10 秒。

升级部署前应为所有 API 实例提供可用的 NATS URL，并让已有 SSE 客户端在切换后重新连接、通过历史 API 同步最终事件。Redis 仍是其他运行时能力的依赖，但不再承载 Session SSE 事件传输。

NATS 连接关闭 reconnect publish buffer，避免断线期间累积的过期 preview 在重连后发送。Core 发布失败由已有 fanout 边界记录；未来可靠 producer 必须自行使用 JetStream ACK、稳定幂等键与重试，不能依赖连接缓冲实现可靠投递。fanout 只维护按 subject 的共享 subscription state 和本地 SSE 引用计数；消息排队、异步回调串行化和断线后的 subscription 恢复都由 nats.go 负责。确认首次订阅的 NATS round trip 在 registry 锁外执行，同 subject 的并发订阅共享一次确认。fanout 在连接 drain 前关闭自己的订阅，不关闭其他组件共享的连接。具体事件与故障合同见 [Worker SSE fanout](ccrv2/ccr-v2-worker-sse-fanout.md)。

## 本地拓扑

Docker Compose 使用 `docker.io/library/nats:2.14.6-alpine` 启动三个 JetStream 节点。节点共用 `oma-nats` cluster name，通过 Compose 内网的 `6222` route 端口互联，并分别使用 `natsdata`、`natsdata2`、`natsdata3` named volume；三个节点不得共用 store directory。客户端和监控端口只绑定宿主机 loopback：

- `127.0.0.1:4222` / `4223` / `4224`：三个 NATS 客户端种子地址。
- `127.0.0.1:8222` / `8223` / `8224`：各节点的监控与 `/healthz?js-enabled-only=true` 健康检查。

当前本地拓扑不启用认证或 TLS，因此不得把上述端口发布到非受信网络。三节点只提供本地故障切换拓扑，不等于生产安全配置。生产部署必须使用独立账号与凭证、TLS、网络策略和资源配额；凭证可由 NATS URL 或后续独立凭证字段承载，但不得写入日志或受跟踪示例。后续创建 JetStream stream 时还必须显式选择合适的 replica 数；集群不会自动把单副本 stream 变成三副本。

## 后续接入约束

可靠业务队列接入前必须先定义 subject 命名、消息 schema/version、幂等键、投递与 ack 策略、重试/backoff、dead-letter 流和租户隔离。Core NATS 的 at-most-once 语义不能被误当成持久队列；需要持久化、重试的路径必须使用 JetStream，并为失败和重复投递补充测试。当前没有创建 stream 或 consumer，SSE preview 不落 JetStream；不要创建捕获 `oma.s.>` 的 stream 而意外持久化预览内容。

## 验收

连接层测试以内嵌 NATS Server 分别覆盖空 URL、JetStream 未启用和成功连接。Compose 验收使用：

```bash
docker compose up -d nats nats-2 nats-3
docker compose ps nats nats-2 nats-3
curl --fail 'http://127.0.0.1:8222/healthz?js-enabled-only=true'
curl --fail 'http://127.0.0.1:8223/healthz?js-enabled-only=true'
curl --fail 'http://127.0.0.1:8224/healthz?js-enabled-only=true'
just generate
TEST_NATS_URL=nats://127.0.0.1:4222,nats://127.0.0.1:4223,nats://127.0.0.1:4224 go test ./internal/sessions -run TestNATSFanout -count=1 -v
```
