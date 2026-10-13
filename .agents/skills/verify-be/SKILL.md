---
name: verify-be
description: 验证 OMA 后端部署与 River 执行、终态、事务副作用、重试、重复任务和重启恢复。验证记忆与文件存储挂载、清理、回复记录归档、文件生命周期，以及聊天消息、工具审批、事件流、历史和性能。使用 Go 命令及隔离依赖。按场景选择真实服务或工作进程。
---

# 后端验证

在仓库根目录运行 `.agents/skills/verify-be/scripts/verify-be`。
脚本生成 Yourbatis 源码，构建并执行 `cmd/verify-be`。
编排和结果判断由 Go 完成。
也可先运行 `./scripts/generate-go.sh`，再构建并运行 Go 命令。
技能文件位于本项目的 `.agents/skills`。

本地场景验证后端行为和本地沙箱启动。
云端适配器场景单独验证服务商操作。
浏览器展示需要另行检查。
纯文档修改无需启动验证环境。
执行前，必须取得用户对后端验证的明确要求。

## 验证流程

1. 阅读对应[功能地图](features/README.md)，选择受影响的最小场景集合。
2. 运行 `just verify-be DOMAIN doctor`，检查对应领域的依赖。
3. 完成源码修改和仓库要求的质量检查。
4. 运行 `just verify-be DOMAIN SCENARIO`。
5. 等待场景完成及资源清理。
6. 读取最终报告。
7. 报告实际场景、结果、报告路径和未覆盖范围。

依赖检查成功只说明环境可用。
验证阻塞时，报告缺少的依赖。
场景运行期间保持源码不变。
生成、构建和会重建 Mapper 的检查必须与场景串行运行。
性能比较期间避免其他本地负载。

| 修改内容                                             | 需要运行的场景                                                          |
| ---------------------------------------------------- | ----------------------------------------------------------------------- |
| 部署创建、River 调度、重试、幂等或重启恢复           | 下文四个 `deployment` 场景                                              |
| 记忆与文件存储的挂载、写入、作用域、生命周期或清理   | 下文六个 `memory` 场景。实际挂载先运行 `memory doctor mounts`           |
| 回复记录归档、导出、恢复、删除、压缩边界或对象完整性 | 下文五个 `transcript` 场景                                              |
| 文件接口、元数据、授权、存储或清理                   | 九个本地 `files` 功能场景及 `files.performance`                         |
| 工具权限或审批                                       | `chat.tools`                                                            |
| 忙时拒绝、重试、ACK、重连、工作进程替换或历史恢复    | `chat.reliability`                                                      |
| NATS 广播或跨后端实例交付                            | `chat.instances`                                                        |
| 会话创建、后台执行器、挂载或工作进程启动             | `chat.public`                                                           |
| 子配置、子技能、MCP、工具权限或原线程续聊            | `chat.subagents`。阅读[子智能体说明](features/subagents.md)             |
| 聊天查询、调度、流式延迟或其他性能行为               | `chat.performance`，并比较基线                                          |
| 上游鉴权错误或失败回合结束                           | `chat.upstream-errors`。阅读[上游错误说明](features/upstream-errors.md) |

聊天行为修改必须包含 `chat.roundtrip`。
跨边界修改应增加对应场景。
共享文件验证编排修改还需运行聊天回归。
云端场景必须显式提供私有配置，并单独运行。
场景验证不能替代仓库要求的静态检查和单测。
场景契约变化时，同步修改功能地图和设计文档。

## 性能比较

按[性能流程](features/performance.md)选择基线。
基线应使用 PR 基础提交或修改前的提交。
只有 `HEAD` 未包含待评估修改时，才能将它作为基线。

1. 使用 `--backend-ref REF` 测量基线提交。
2. 使用 `--baseline REPORT` 比较候选版本。
3. 若发生退化，另运行 `--diagnostics` 定位原因。
4. 修复原因后，再次比较同一基线。

诊断运行不能用于基线比较。
不要替换基线或放宽阈值来掩盖退化。
交付时列出基线提交和报告。

## 命令与依赖

运行 `just verify-be -h` 或 `--help` 查看命令。
运行 `just verify-be chat -h` 查看聊天场景。
运行 `just verify-be chat tools -h` 查看工具场景。
也支持 `help`、`help doctor` 和 `help chat tools`。

帮助命令在读取运行配置及检查 Git、Docker 前退出，退出码为 0。
脚本入口仍需要 Go 来构建命令。
Cobra 处理命令和参数。

- 所有场景支持 `--timeout`。
- `chat`、`files generated` 和 `memory mounts` 支持 `--worker-image`。
- 两个性能命令支持 `--baseline`、`--backend-ref` 和 `--diagnostics`。
- 云端命令及其依赖检查支持 `--cloud-config`。
- 长参数使用两个短横线。帮助参数可使用 `-h`。

本地运行需要 macOS 或 Linux、项目 Go 工具链、Bash、Git、tar 和 Docker Compose。
Docker 必须有足够空间运行三个 JetStream 副本。
以下镜像必须已存在于本地：

- `postgres:17`
- `redis:8`
- `nats:2.14.6-alpine`
- `pgsty/minio:latest`

聊天还需要包含 `/opt/claude-code/bin/claude` 的工作进程镜像。
`files generated` 和 `memory mounts` 还需要 FUSE。
其他文件与记忆场景不使用工作进程配置。
命令不会拉取镜像。
报告记录镜像 ID。不同镜像不能用于同一次性能比较。

临时签名密钥由 Go 标准库生成。

```sh
.agents/skills/verify-be/scripts/verify-be chat doctor
.agents/skills/verify-be/scripts/verify-be chat roundtrip
.agents/skills/verify-be/scripts/verify-be chat subagents
.agents/skills/verify-be/scripts/verify-be chat tools
.agents/skills/verify-be/scripts/verify-be chat reliability
.agents/skills/verify-be/scripts/verify-be chat instances
.agents/skills/verify-be/scripts/verify-be chat public
.agents/skills/verify-be/scripts/verify-be chat upstream-errors
.agents/skills/verify-be/scripts/verify-be chat performance
```

工作进程镜像按以下顺序选择：

1. `--worker-image IMAGE`。
2. `OMA_WORKER_CONTROL_IMAGE`。
3. `.verify-be.local.json` 中的 `worker_image`。
4. `ghcr.io/superduck-ai/managed-agent-sandbox:latest`。

本地配置文件已被 Git 忽略。
私有镜像地址只保存在该文件中。
配置后无需再设置环境变量。
帮助输出不显示实际配置值。

命令生成源码并构建实际后端。
后端使用 `127.0.0.1:18080`，启动后等待 `/readyz`。
每次运行使用独立 Compose 项目和临时依赖。
临时依赖包括 PostgreSQL、Redis、三个 NATS 节点和 MinIO。
命令不读取 `config/config.yaml`，也不停止占用端口的其他进程。
依赖端口由 Docker 分配。

## 依赖检查

`doctor` 检查工具、Docker、镜像、18080 端口和 Docker 卷空间。
可用卷空间必须至少为 1 GiB。
检查会短暂创建探针容器和匿名卷，并在结束时删除它们。

以下场景还检查沙箱内的 FUSE 和挂载权限：

- `chat doctor public`
- `chat doctor subagents`
- `chat doctor upstream-errors`
- `files doctor generated`
- `memory doctor mounts`

每次场景运行都会执行对应依赖检查。
随后检查依赖健康状态和后端就绪状态。
将验证命令指向其隔离环境，不要指向已有 API 或数据库。
运行期间修改源码会使结果失效。

## 全量 Go 测试

运行 `just verify-be test` 验证默认构建的全部 Go 包。
环境使用临时 PostgreSQL、Redis、NATS 和 MinIO。
测试命令为 `go test ./... -json -count=1`。
默认每个包的期限为 15 分钟，可用 `--timeout` 覆盖。

该命令只启动依赖，不启动独立后端。
它提供临时配置和签名密钥。
它启用迁移、Redis、S3 适配器和事件内容 S3 集成测试。
子进程会移除继承的外部测试地址和云端启用变量。
结束时删除容器、卷和私有配置。

报告列出失败和跳过的测试。
迁移、S3 和 Redis 必需测试必须通过。
`passed_with_skips` 表示命令完成，但仍存在未验证项。
退出码 0 不代表被跳过的测试通过。
普通功能场景仍拒绝所有跳过项。

真实文件和聊天场景需要单独运行。
云端、`e2e` 构建标签及外部 SDK 测试不在该命令范围内。

## 部署与 River 场景

阅读[部署说明](features/deployment.md)。
运行 `deployment doctor` 后，再运行 `lifecycle`、`retry`、`idempotency` 和 `restart`。
这些场景使用生产接口、定时工作进程、真实 River 和临时 PostgreSQL。

检查执行、会话、线程、工作、事件和文件存储的一致性。
检查非法引用、计划删除、旧任务、重复任务、重试耗尽和提交前后恢复。
`restart` 使用独立进程，任务超时为 10 秒，恢复阈值为 15 秒。
生产环境的一小时恢复延迟与会话模型执行仍未测量。
这些场景不需要模型工作进程或 FUSE。
默认期限为 3 分钟，`restart` 为 5 分钟。

## 记忆与文件存储场景

阅读[记忆说明](features/memory.md)。
运行 `memory doctor` 后，再运行 `integrity`、`isolation`、`cleanup`、`lifecycle` 和 `filestore`。
前五个场景使用生产接口、独立数据库命名空间和真实 MinIO。
普通 Go 套件会显式跳过这些需单独启用的场景。

另运行 `memory doctor mounts` 和 `memory mounts` 检查真实挂载。
该场景使用后台执行器、Docker 和 FUSE。

删除存储或脱敏版本时，清理任务入队失败必须回滚元数据。
临时 MinIO 桶必须未启用版本控制。
记忆历史使用独立对象键。
删除对象应返回 NotFound，保留对象仍可读取。

故障只在测试端注入上传、删除或入队失败。
测试调用真实清理工作进程的 `RunOnce`。
测试显式调整重试资格和待处理孤立对象的保护期限。
这些操作不验证真实时钟退避或后台自动调度。

`mounts` 在沙箱分配后注入配置失败，检查沙箱删除。
成功流程直接在容器内执行文件操作，不发起模型回合。
销毁沙箱后，另一会话必须读取持久存储并重建本地目录。
云端分配、IAM、浏览器、进程崩溃、令牌续期、并发编辑和性能仍未验证。

## 回复记录归档场景

阅读[归档说明](features/transcript.md)。
运行 `transcript doctor` 后，再运行 `integrity`、`recovery`、`concurrency`、`boundary` 和 `lifecycle`。
这些场景使用独立数据库命名空间和真实 MinIO。
生产保留服务及私有历史处理器在测试进程内运行。
`lifecycle` 还构建并执行实际维护命令。
这些场景不需要独立后端、工作进程或 FUSE。

上传钩子和命名空间内触发器注入故障。
测试显式调整记录时间，并调用 `RunOnce` 清理。
River 周期扫描、自动重试、进程崩溃恢复、云端 IAM 和吞吐量仍未验证。
`lifecycle` 和 `recovery` 默认期限为 5 分钟，其他场景为 3 分钟。

## 文件场景

阅读[文件说明](features/files.md)。
本地功能场景为 `invalid`、`isolation`、`lifecycle`、`storage`、`attachments`、`platform`、`recovery`、`exhaustion` 和 `generated`。
性能比较使用 `files performance`，流程见[文件性能说明](features/files-performance.md)。

每个场景使用临时后端、数据库和 MinIO。
文件上限为 64 KiB，工作区配额为 96 KiB。
本地 Go 代理保留 S3 请求，可按测试路径注入 403 或断开连接。
后端不增加生产故障开关。

普通上传继续返回 `downloadable=false`。
`lifecycle` 使用单独的可下载测试记录。
`generated` 通过 SDK 消息触发已允许的真实 `Write`。
该场景检查后台执行器、FUSE、文件投影、公开资源、列表和字节一致性。
上游模型响应由脚本提供。
先运行 `files doctor generated` 检查依赖。

`recovery` 注入删除及配额补偿失败。
测试观察自动后台尝试，恢复存储后等待真实的一分钟退避。
该场景不手动创建或推进清理任务。
`attachments` 检查删除冲突、解除引用、过期可见性、归档保留和会话删除清理。
`platform` 使用隔离开发登录流程，检查 base64、预览、缩略图和派生对象清理。

`storage` 检查分段上传中断、无效凭据和指定版本或全部版本删除。
这些本地 S3 检查不验证云端 IAM 或网络设施。

`exhaustion` 在十次删除失败前显式提前执行测试任务。
测试检查次数、状态、退避、终态、显式恢复和完成后的幂等性。
它不等待累计 199 分钟的完整计划。
真实的一分钟等待由 `recovery` 单独验证。

## 云端与聊天执行

云端命令为 `files cloud-storage` 和 `chat cloud-renewal`。
执行前阅读[云端说明](features/cloud.md)。
必须显式提供私有测试配置。
配置缺失时，命令报告阻塞，不隐式读取生产或开发配置。
场景只创建有数量限制且归属本次运行的资源。

失败后仍必须回收这些资源。
云端断言直接由 Go 命令产生，不依赖 Go 测试 JSON。
云端场景不需要本地 Docker 或工作进程。

`chat roundtrip` 对新构建后端执行 `TestChatRoundtrip`。
真实工作进程调用真实 `/v1/messages` 代理。
模型脚本等待 SSE 客户端看到中文片段后，才继续输出。
该检查不用任意休眠替代事件断言。
此测试固定预览先于最终回复的顺序。
生产流量中，最终回复可能先于最后一条预览保存。

多数场景通过已有服务或数据库接口激活会话与工作进程记录。
这些场景关闭后台执行器，避免竞争工作队列。
智能体创建、环境创建、消息、协议、事件流、模型代理和历史使用真实 HTTP。

`chat.public`、`chat.subagents` 和 `chat.upstream-errors` 使用公开会话接口和实际后台执行器。
它们使用本地 Docker 服务商实现、真实挂载和环境管理器。
本地 E2B 控制接口检查容器归属，并处理连接、期限和删除。
云端 TTL 续期不在这些本地场景范围内。
阅读[公开启动说明](features/public-start.md)和[上游错误说明](features/upstream-errors.md)。

`chat tools` 先拒绝格式错误的 Bash 调用。
测试检查文件未生成，以及公开 SSE 和历史中的调用、错误结果 ID 和顺序。
下次模型请求必须包含未解析输入和错误结果。
回合必须以空闲状态及 `end_turn` 结束。

随后由官方 `anthropic-sdk-go` 发送消息和工具确认。
真实工作进程仅在确认后执行 `Write`。
先拒绝，再允许。
检查确认前文件不存在、允许后的字节、历史关联和队列清空。
SDK 本身不执行工具。
详见[工具说明](features/tools.md)。

`chat reliability` 检查忙时拒绝不落库、空闲重试、SSE 重连和中断后历史恢复。
它还检查工作进程替换、凭据轮换和旧凭据拒绝。
`chat instances` 将输入与工作进程流量和 SSE、历史分配到不同后端进程。
详见[恢复说明](features/recovery.md)。

`chat performance` 使用固定串行负载。
先用 `--backend-ref REF` 测量基线，再用 `--baseline REPORT` 检查候选版本。
`--diagnostics` 另行采集后端性能资料。
详见[性能契约](features/performance.md)。

## 报告与判定

命令打印 `tmp/verify-be/<run-id>/` 的绝对路径。
`report.json` 保存机器判定，`report.md` 保存范围和时间线。
`tests.jsonl` 保存 Go 测试断言和阶段标记。
`tests.stderr.log` 保存 Go 工具诊断。
其他诊断文件包括 `server.log`、`build.log`、`dependencies.log` 和 `cleanup.log`。

原始证据仅供本地使用。对外只分享摘要和非敏感元数据。

本地场景通过必须同时满足以下条件：

- 指定测试和包通过。
- 所有必需阶段存在。
- 没有跳过项。
- 源码指纹保持不变。
- 清理成功。

云端场景必须完成直接断言、必需阶段、源码检查和清理。

| 退出码 | 含义                                 |
| ------ | ------------------------------------ |
| `0`    | 场景通过、依赖检查成功或帮助命令成功 |
| `1`    | 场景失败或性能退化                   |
| `2`    | 参数无效、基线不兼容或依赖阻塞       |

跳过、无匹配测试、启动失败和未完成运行不能报告通过。
阶段时间只是观察值。
性能退化结论只来自性能基线比较。

CI 分别运行聊天、文件、归档、记忆和部署验证。
部署任务运行四个 River 场景。
归档任务运行五个场景。
记忆任务运行五个不需要工作进程的场景。
聊天任务还运行 `memory mounts`、`files generated` 和可靠性场景。

CI 在同一主机测量 PR 基础提交并检查候选版本。
CI 先拉取镜像，再调用不拉取镜像的验证命令。
分支保护需单独要求以下任务通过：

- `Chat verification`
- `Files verification`
- `Transcript verification`
- `Memory verification`
- `Deployment verification`

工作流本身不会设置分支保护。
CI 只上传 JSON 和 Markdown 判定，不上传原始日志、配置或性能资料。

## 清理

成功、失败、Ctrl-C 和 SIGTERM 都执行清理。
先停止本次后端，再删除带本次标签的工作进程容器。
随后删除本次 Compose 项目和卷。
配置、签名密钥和后端二进制会被删除。
报告保留。

SIGKILL 或主机崩溃后，按输出的运行 ID 查找资源：

```sh
docker ps -a --filter label=oma.verify-be.run=RUN_ID
docker compose -p RUN_ID -f /absolute/evidence/directory/compose.json ps -a
```

1. 用 `docker rm -f` 删除确认归属本次的工作进程容器。
2. 用相同 Compose 参数执行 `down --volumes`。
3. 按本次 `server` 可执行文件路径核对遗留后端进程。
4. 停止确认归属本次的后端进程。
5. 删除本次 `jwt.pem` 和 `config.json`。

保留共享 Redis 数据和共享 NATS 流。
没有最终判定的中断报告不能作为通过证据。

## 辅助检查与期限

```sh
go test ./cmd/verify-be -count=1
```

这些测试检查命令解析、帮助、镜像优先级和无效结果拒绝。
它们还检查配置隔离、依赖拓扑、取消、探针、清理、下载上限和报告可比性。
契约变化时，同步修改功能地图和设计文档。

| 场景                         | 默认期限 |
| ---------------------------- | -------- |
| 消息收发、工具、跨实例       | 3 分钟   |
| 可靠性、聊天性能             | 5 分钟   |
| 公开启动、子智能体、生成文件 | 6 分钟   |
| 文件恢复、文件性能、云端续期 | 5 分钟   |
| 其他文件场景                 | 3 分钟   |
| 不需要工作进程的记忆场景     | 3 分钟   |
| 实际记忆挂载                 | 8 分钟   |

可使用 `--timeout 8m` 覆盖场景期限。
该期限不包含环境构建与启动。
Go 测试另有 30 秒清理宽限。
外层命令另有 2 分钟编译与退出宽限。
读取 `failure_kind` 区分场景超时、外层超时、依赖阻塞和基线不兼容。
单次超时不足以证明性能退化。

## 命令迁移

使用 `verify-be chat roundtrip` 替代 `verify-chat run chat.roundtrip`。
旧命令不保留别名。
本地工作进程设置迁移到 `.verify-be.local.json`。
报告和资源标签使用 `verify-be`。
已有证据仍保留在原目录。
根 `doctor` 只检查公共依赖。
