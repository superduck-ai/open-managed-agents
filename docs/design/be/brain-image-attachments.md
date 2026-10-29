# Brain 远端图片附件与原生图片预算

状态：已实现消息图片与 Read 图片工具结果闭环。公开事件和消息附件物化合同仍见 [message-media-attachments](message-media-attachments.md)。不考虑回滚。

## 黑盒验收方案

以公开可观察的输入、模型请求、日志恢复和 HTTP 响应为验收边界。先覆盖失败，再覆盖成功；普通测试使用进程内 API、模型和对象存储 fake，数据库扫描和事务测试使用 integration 标签与隔离 PostgreSQL。真实 Worker 与视觉模型验收仅在用户明确要求后使用 verify-be 的最小场景集合。

| 场景 | 输入或故障 | 可观察结果 |
| --- | --- | --- |
| 无效上传 | 摘要不符、MIME 不符、超限或截断 | 400/413，无可读附件；不覆盖已有正确对象 |
| 隔离 | 其他 Session/workspace 请求相同摘要，旧 worker epoch | 404/409；本地缓存不能绕过当前 Session 的授权 |
| 源不可用 | 消息私有附件 404，图片损坏或像素超限 | 仅该图片替换为附件不可用文本，其他内容顺序保持 |
| 暂时失败 | 源读取或远端上传返回 503 | 不 ACK、不标记输入已处理；重新投递后成功且只有一条原生消息 |
| 图片预算 | 历史图片超过张数或请求编码字节预算 | 请求发出前产生 IMAGE_OFFLOAD_REQUIRED；持久化 image/offload，再次请求省略旧图片并成功 |
| summary 预算 | 压缩摘要包含超预算图片 | summary offload 后恢复，不消耗普通 provider 重试预算 |
| 请求体预算 | 图片与文本/schema 合计超过代理限制 | 可卸载图片被省略；纯文本已超限时明确报错，不无限重试 |
| 完整性 | 缓存或远端对象损坏 | 拒绝错误内容，不调用模型；不会把缓存路径或凭证写入日志 |
| 正常消息 | 文本、私有 file_id 图片、文本交错 | 模型收到原生图片转换的内联变体，顺序保持；公开事件不变，私有日志只含引用 |
| 文本模型 | 模型 input_modalities 不含 image | 模型只收到图片占位；切换视觉模型后可读取未 offload 图片 |
| 重复投递 | 相同图片多次投递、上传成功后进程退出 | 会话内内容寻址幂等；恢复不重复提交已经持久化的输入 |
| 跨实例 | 清空本地缓存，在新 Brain 重放日志 | 使用新凭证读取远端图片；已 offload 图片不读取字节 |
| 清理 | 删除 Session 或 pending 超过保留期 | 创建持久化清理任务，删除全部对象版本后回收登记；失败保留任务重试 |
| 兼容 | document、Claude Code | 保持既有请求语义；旧 oma-image 日志不再兼容 |

## 实现边界

服务端保存归一化不可变图片，复用现有对象存储与 object cleanup worker，不使用 Filestore 保存归一化结果。原始消息附件继续由现有 Filestore 管理。Brain 复用 DSH 编码器生成原生 ImageAttachmentRef 和请求变体，本地文件仅为缓存；不向模型暴露本机路径。

当前覆盖后端 URL 物化、消息 file_id/base64 图片、Read 图片结果、模型预算、原生计量与 offload、跨实例恢复和删除清理。PDF 与普通文件接入留待后续；不迁移旧 oma-image 日志。

上传先在固定大小缓冲内校验字节，再发布对象，防止坏请求覆盖内容寻址键。对象上传在事务外进行，现有数据库事务与行锁只协调状态发布和清理，不新增分布式租约服务。

## 存储与协议

新增 `brain_image_attachments` 表，无外键，唯一范围为 `(workspace_uuid, session_uuid, sha256)`。图片引用使用 DSH 原生 `ImageAttachmentRef`，摘要为归一化字节的 SHA-256。表中保存媒体类型、大小、对象位置和 pending/attached/deleting 状态，不把图片字节写入日志。

Worker 路由位于 `/v1/code/sessions/{code_session_id}/worker`：

| 方法与路径 | 行为 |
| --- | --- |
| PUT `/image-attachments/{sha256}` | 校验长度、摘要、嗅探 MIME 后写入；新建 201，幂等 200，返回 attachment_id/media_type/bytes |
| HEAD `/image-attachments/{sha256}` | 确认当前 Session 所有权，返回 Content-Type、Content-Length、ETag |
| GET `/image-attachments/{sha256}` | 校验保存的摘要、长度和 MIME 后返回原始归一化字节 |
| GET `/message-attachments/{file_id}/content` | 只读取本 Session 的私有消息附件，不回退查询 workspace Files |

所有接口复用当前 worker ingress 凭证，并核对 `X-Worker-Epoch`。会话标识从可信 claims 与 DB 解析，其他 Session 的相同摘要不可访问。存储暂时失败为 503；摘要/类型错误及已存对象校验损坏为 400；不存在或存放在不支持的 bucket 为 404；超限为 413。对象流读取失败与读取成功后确认损坏分开处理，前者允许恢复，后者不反复重试。

`brain_image_attachments.max_object_bytes` 默认为 4 MiB，可降低，不能超过 4 MiB。Brain 使用 DSH 默认的源字节、像素和尺寸限制；逐张入库并累计图片张数和成功接纳的源字节。单图放不下或接纳失败时不扣字节预算，后续较小图片仍可接纳。达到张数上限或已用完字节预算后不再读取后续图片，不拒绝已接受的整条消息。

| HTTP 状态或故障 | 入站消息图片 | Read 图片结果与存量引用 |
| --- | --- | --- |
| 400/413/415/422 | 单图降级为附件不可用文本 | 新 Read 返回永久工具错误；存量引用返回 ATTACHMENT_CORRUPT |
| 404 | 源图缺失或 PUT 被拒绝，单图降级 | 存量引用返回 ATTACHMENT_NOT_FOUND，不补传本地旧图 |
| 401/403/409/410 | 保留输入、不 ACK，交回既有重新认领路径更新 Worker 凭证或租约 | 不在同一凭证上局部重试；返回明确的重新分配提示 |
| 网络错误、408/429、5xx | 保留输入、不 ACK，由投递恢复重试 | Read 仅对已取得的图片结果接纳重试，最多 3 次，间隔 100/200 ms；不重新执行 MCP |
| 其他拒绝状态 | 正常失败，不作为图片校验错误降级 | 正常失败 |

可恢复错误保持 RetryableInputError 类型与 RETRYABLE_INPUT 工具错误码。Read 重试耗尽后保留该结构化错误及明确的重试提示，不包装成 unsupported result。401/403 在此处是服务端签发的 Worker ingress 凭证错误，与用户图片内容无关；重新认领与同一请求上的存储重试分开处理。

Pending 登记先持久化；上传在事务外进行，有 30 秒超时，上传完成后短事务锁定该行并推进 attached。若上传期间登记已进入 deleting 或已被清理，拒绝发布。事务外上传与清理恰好交错时可能留下迟到的未引用对象，严格消除该窗口需要另行确认上传租约或清理握手的范围，本轮未增加这类机制。清理调度分别选择过期 pending 与软删除 Session 的图片，使用 SKIP LOCKED；标记 deleting 与创建任务同事务提交。新增 migration 00109 为后者添加部分索引。只有 brain_image_attachment 类型的任务完成时关联删除图片登记，其他任务直接完成。任务删除全部对象版本，没有固定 TTL。Session 按既有软删除流程保留登记，未增加物理删除 Session 后的孤儿扫描。

本地缓存按内容寻址共享。每个 Session assignment 与 worker epoch 首次使用哈希时经 PUT 或 HEAD 确认授权，后续复用内存结果；epoch 改变或 assignment 替换后重新确认，其他 Session 不共享授权。授权集合使用弱引用，不保留已释放的 assignment。只有 saveImage/readImage 操作授权集合，通用 remote HTTP 函数不按路由或方法推断缓存行为。远端 GET 与本地读取均检查完整性，imageHostPath 不暴露 Brain 路径。归一化和请求变体处理共用两个并发槽，释放时先将名额直接交给已等待的调用者。本地缓存清理、指标聚合和 Session 图片总配额暂未实现。

## 模型预算与计量

模型目录支持可选 `image_input`，通过现有 provider JSON 和 session_context.models 下发。控制台编辑供应商会保留该字段，本阶段不新增预算表单。字段为零或省略时使用缺省值：

| 字段 | 缺省值 | 单位 |
| --- | --- | --- |
| max_images | 20 | 张，按出现次数计数 |
| max_request_image_bytes | 12582912 | 请求图片编码字节总量，base64 前 |
| max_image_bytes | 1048576 | 单张变体编码字节目标 |
| max_long_edge | 2048 | 像素 |
| max_pixels | 1048576 | 像素面积 |
| tokens_per_megapixel | 1600 | 估算视觉 token / 百万像素 |

目录值需是 0 至 33554432 的整数；这些缺省值是平台保守策略，不是上游能力或计费保证。视觉模型必须显式声明 input_modalities 包含 image，否则 DSH 投影为文本占位；Read 与适配器计价均以未声明 image 为不支持图片。

适配器先按张数决定是否卸载，再使用 DSH readImageRequest 按面积和长边生成变体。字节预算调用 requiredImageOffload，representation=base64，因此预算转换为 base64 后的字节。计量使用同一尺寸策略；文本模型和 offloaded 图片只计实际占位文本。

请求还预留 128 KiB 协议开销并估算其余 prompt/schema；SDK 序列化后用 MAX_MESSAGES_REQUEST_BODY_BYTES 检查实际 32 MiB 请求体，与 Go MaxMessagesRequestBodyBytes 的一致性由合同测试检查，offload 错误码统一引用 DSH IMAGE_OFFLOAD_REQUIRED_CODE。可卸载图片超限时抛 IMAGE_OFFLOAD_REQUIRED，由已挂载插件记录 image/offload，普通轮次和 summary 均可重试。卸载文本不包含本机路径或重新上传要求；纯文本已超限时明确失败。此检查只约束 Brain 发送的请求，document 后续由代理展开造成的膨胀仍是已知限制。

新实例重放远端 dsh 日志后，用当前凭证从远端读取图片并重新生成变体。本地缓存不影响正确性，已经 offload 的图片无需读取字节。URL 图片在投递前已转换为私有 file_id；PDF、普通文件接入及历史 oma-image 迁移不在此阶段范围。

## 验证记录

2026-10-27 本地验收：

- `go generate ./...`、`just test`、`just lint`、`just dead-code`、`just duplicates`、`just complexity`、`just large-files` 通过。
- 隔离 PostgreSQL 17 上执行 `just test-integration ./internal/db -run '^TestBrainImageAttachmentsPostgres$' -count=1` 通过，覆盖真实扫描、pending 重试、幂等、范围隔离、删除调度与清理完成。测试容器已停止。
- Brain 全套普通测试 646 通过、5 跳过。随后针对最终请求体边界执行图片、两种协议适配器与 compaction-budget 共 60 项测试，全部通过；typecheck 通过。
- 前端供应商相关 63 项测试与 `bun run build`、Prettier、命名检查通过。前端全套为 1524 通过、2 失败；失败涉及供应商表单焦点与子 Agent 搜索等待，两项独立重跑均通过，全套仍保留失败记录，未修改无关交互。
- 未运行 verify-be 或真实模型 E2E。未验收真实上游图片限额、计费、生产性能与 S3 多版本物理删除；本轮数据库集成验证不能代替这些验收。


## Read 图片工具结果黑盒验收

沿用 Rust MCP 的 read(file_path)，不增加工具或沙箱接口。普通测试使用真实 Brain loop、进程内 MCP/模型和附件 API fixture。

| 场景 | 可观察结果 |
| --- | --- |
| Read 返回损坏图片或附件上传失败 | 工具结果为错误文本，无原始字节进入日志；不会因转换失败自动重放已经完成的 Read |
| 审批拒绝 | 不调用沙箱，不保存图片，不向模型发送图片 |
| 当前模型未声明 image | Read 图片结果成为可理解的工具错误，不存储或发送图片 |
| Read 返回文本、图片、文本 | 原生 tool/result 按顺序保存文本与 attachment 引用；上传成功后才记录结果；下一次模型调用收到图片 |
| Messages | 图片位于对应 tool_result.content 内，保留 tool_use_id |
| Chat Completions | tool 消息保持文本，图片放在全部连续 tool 结果之后的 user 消息，并标明来源 call ID |
| 并行工具 | 所有 tool call 都有对应结果，图片消息不插在尚未返回的 tool 结果之间 |
| 跨实例恢复 | 空缓存新实例从远端读取已保存图片，不重跑 Read；恢复后的模型请求仍包含图片 |
| 图片预算 | 工具图片参与原生计量与 image/offload，超预算后省略旧图片并继续模型调用 |
| 普通文本 Read 与其他工具 | 保持已有文本结果、审批和执行语义 |

本轮不修改 Rust 图片编码器、不新增 PDF 或其他二进制处理，不运行 verify-be 或真实 Worker/模型验收。

Read 的图片内容在 `sandbox-tools/read.ts` 完成 MCP 数据校验和远端 attachment 入库，通用 ToolExecution 只接收结果转换函数，继续负责原有审批、派发与错误处理。仅 Read 使用原生内容数组，其他沙箱工具与外部 MCP 的文本合同不变。损坏图片和图片上传失败会成为本次工具错误，不把已完成的沙箱读取当作未知执行结果，也不隐式重放。

Messages 将图片序列化到对应的 tool_result.content。Chat Completions 的 tool 消息仍为文本，连续工具结果收齐后追加一条 user 消息，按工具调用 ID 标明各图片来源；这一调整只发生在请求投影中，不额外持久化 user 消息。工具图片使用相同的原生计量、请求变体和 offload 策略，卸载后只保留占位文本。读取与保存使用实际执行 Agent 的 Session ID，子线程通过既有映射使用根 Session 的远端权限。

工具结果标记为 `isError` 时，请求投影先生成只含原有文本的副本，再计算图片预算或读取附件。两种后端都只输出 `error-text`；错误结果中的图片不读取字节、不占用请求图片预算，也不进入 Chat 的图片 user 消息。原始 DSH 消息和日志不变。`read-images.test.ts` 覆盖错误结果、两处图片 flush、混合工具/内容顺序、offload、默认路由和原生日志隔离。

Read 图片链路验收记录（2026-10-27）：先新增黑盒测试，初次执行为 2 通过、6 失败；实现后 9 个 Read 场景全部通过，连同附件、工具执行与 Chat 恢复的定向回归共 28 项通过。完整 Brain 测试为 656 通过、5 跳过，typecheck 与 build 通过。`just test`、`just duplicates`、`just complexity` 通过。跳过项需要真实 Rust MCP 二进制；本轮没有运行真实 Worker、模型或 verify-be，不能作为这些外部依赖的集成验收结果。

### 独立可执行文件的图片依赖

DSH 在首次图片操作时动态加载 Sharp。独立构建将该调用改为可静态打包的入口，并嵌入当前构建平台的 Sharp 原生模块与 libvips。首次加载时按构建内容摘要释放到用户的 `~/.cache/oma-brain/native/`，保留两个原生包的相对目录，随后加载原生模块。部署仍只分发可执行文件，不依赖部署机器的 `node_modules`。缓存可删除，下次首次读图重新生成。图片真实验收必须使用此独立可执行文件。

### 2026-10-27 真实链路验收

按用户要求仅运行 `chat.brain-images` 与必要的 `chat.roundtrip`，对应 doctor 均通过。图片场景使用真实 kimi-k2.5 的 Messages 后端、独立 Brain 可执行文件、Docker Worker/FUSE、PostgreSQL 与 MinIO。两个不同颜色的随机 PNG 分别通过消息附件与 Session 文件挂载进入链路；模型正确回答两个颜色，且公开历史记录 `read` 工具。私有 `user/message` 与 `tool/result` 均包含原生图片引用，原图 base64 未进入私有日志；远端对象摘要和字节数校验通过。

本次发现并修复独立构建漏打包 Sharp/libvips 的问题。另发现旧测试 Worker 不支持当前 Runner 的 `--claude-agent-version` 参数；切换本机兼容镜像，并补充 doctor 的启动参数检查。新场景的公开 events 请求封装、CodeSession 初始化顺序及公开工具名断言也已修正。

| 场景 | 结果 | 报告 |
| --- | --- | --- |
| `chat.brain-images` | pass，三个必需阶段通过，无 skip，清理完成 | [Markdown](../../../tmp/verify-be/verify-be-c408dcc886ee/report.md)、[JSON](../../../tmp/verify-be/verify-be-c408dcc886ee/report.json) |
| `chat.roundtrip` | pass，五个必需阶段通过，无 skip，清理完成 | [Markdown](../../../tmp/verify-be/verify-be-53bf29cb65b2/report.md)、[JSON](../../../tmp/verify-be/verify-be-53bf29cb65b2/report.json) |

Worker 镜像摘要为 `sha256:b3a09eb84545362778315d0364affc2368d638f28555f783f4befdc97d6b0d3c`。已同步主分支并将新增 migration 编号更新为 `00108`，随后重跑上述两个场景。验收源码在运行期间保持不变；本节在两轮报告和清理完成后补充。报告位于本地忽略目录，不进入版本控制。

未覆盖其他模型与 Chat Completions 后端、真实模型超预算 offload/summary、跨实例恢复、云端沙箱、计费或性能基线。`chat.roundtrip` 使用脚本上游模型，其通过不作为额外的真实视觉证据。

### 2026-10-28 审查修正

修复审查项 1、3 至 10。资源包错误通过 sentinel、apperr.New 和 HTTP ErrorAdapter 输出，413 使用统一的 invalid_request_error。补充永久错误单图降级与输入确认、瞬时错误重试、批次限额、授权范围与 epoch 失效、压缩名额交接、缺省图片能力、最终请求体错误码及跨语言大小合同测试。PostgreSQL 回归覆盖上传不占连接、过期和软删除两类调度，以及非图片任务不访问图片表。

审查项 2 按用户要求暂不修改：切换前仅存在 Brain 本地的原生图片，远端没有登记时仍不可解析。旧 oma-image 类型及编码路径已删除，不再提供历史兼容。本轮未重新运行 verify-be，之前的真实链路报告保留为当时版本的记录。

本轮验证：`just test`、`just lint`、`just dead-code`、`just duplicates`、`just complexity`、Brain typecheck 与独立构建通过。Brain 全套 675 通过、5 跳过，随后最终图片边界组 25 项通过。隔离 PostgreSQL 17 执行 `just test-integration ./internal/db -run 'TestBrainImageAttachmentsPostgres|TestObjectCleanupJobsPostgres|TestEventPayloadBlobsPostgres' -v`，三个测试组通过，无跳过；未连接开发环境依赖，测试容器已停止。

### 第二轮审查验证

已通过回归复现并修正永久存储损坏/错误 bucket 的 503 分类、网关与 Worker 凭证失败丢输入、Read 接纳错误被吞没、超限或失败图片占用后续图片预算。Read 的传输恢复只重试图片保存，保留 MCP 执行次数为一次；工具注册表保留 RETRYABLE_INPUT 结构化错误码。请求构建、assignment 校验和 Console 校验统一复用所在语言的代理上限常量。

清理查询没有修改：在真实 PostgreSQL 17 的隔离 schema 中，以 20000 个无关已删除 Session 和 20000 张属于同一活跃 Session 的图片执行 EXPLAIN ANALYZE，Session 索引查找次数为 1，没有访问无关已删除 Session 行，单次约 3 至 4 ms。新增 integration 回归守卫这一查询形态；该结果不代表生产性能基线，也不能保证其他数据分布下的扫描量。不能仅据 JOIN 写法断言每个 tick 必然扫描全部已删除会话。

旧本地图片兼容与事务外迟到上传的严格回收仍按已确认范围不扩展。存量引用缺失仅修正为 ATTACHMENT_NOT_FOUND，没有增加本地补传、上传租约或清理握手。

本轮验证：新增边界测试在 f76af34 上先复现 17 个失败断言。修正后的定向组 66 项通过，随后 Brain 全套 700 通过、5 跳过，类型检查、独立构建通过；架构检查保持通用 AI SDK 适配器只依赖 DSH/AI SDK，OMA 的请求体上限通过路由字段传入。Go 默认单测及 lint、dead-code、duplicates、complexity 均通过。隔离 PostgreSQL 17 的清理执行计划回归通过，测试容器已停止。本轮未运行 verify-be 或真实模型。
