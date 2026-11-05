# DSRv1 逐接口调用与请求响应评审

评审日期：2026-11-03。结论：14 个主接口都有明确的 Brain 调用职责，但需要修订部分请求、响应和错误处理。第 15 节的运行时能力还依赖 MCP、附件和 OTLP 原生协议，不能统一套用 JSON 请求、Anthropic 错误体或自动重试。

本报告覆盖接口参考中的全部接口，包括展开后的附件、MCP metadata、模型和沙箱侧路由。本文保留实现前评审。已新增并接通 `src/dsr/` 原生 host，默认使用 DSRv1，支持显式 CCRv2 回退，当前逐接口实现状态见 [实验实现状态](implementation.md)。当前代码路径相对于 `oma-brain-dsh/src/`，服务端路径相对于仓库根目录。版本依据与第一轮恢复探针见 [兼容性验证](compatibility-review.md)。

## 1. 验证方式和结果

本次逐条核对请求字段来源、响应消费者、现行函数、执行顺序、并发和失败处理，并运行整个 Brain 普通测试目录及接口形状探针。

| 执行内容 | 结果 | 能证明的范围 |
| --- | --- | --- |
| `cd oma-brain-dsh && bun test ./tests --timeout 15000` | 805 pass、5 skip、0 fail，25624 次断言，84 个文件 | 当前 CCRv2、claim、模型、工具、附件、问答、复制、遥测和子线程行为 |
| `bun test ./tmp/dsrv1-review/interface-shapes.test.ts --timeout 15000` | 4 pass、0 fail，7 次断言 | 当前 trace 读取要求数组；DSR claim 不能直接交旧准入校验；普通 HttpError 会丢 DSR 错误头 |
| 第一轮审批顺序探针及定向测试 | 21 pass、0 fail | 审批前已存在远端 tool/call；现行恢复和复制屏障 |

上述执行互有覆盖，不能相加为互不重复的用例数。原始输出在 `tmp/dsrv1-review/brain-suite-output.txt`、`interface-shapes-output.txt` 和 `test-output.txt`，临时探针未纳入版本控制。

5 个 skip 分别为 Rust 沙箱工具定义一致性、丢失 MCP server 的结果分类、普通 Bash 取消、用户中断 Bash、普通工具完成。本次没有把这些项计为通过。

以下测试数字是实现前的评审基线。本轮已补齐 14 个端点、审批表、turn reducer 与原生 host，并新增 PostgreSQL、原生 SIGKILL 与 `chat brain-dsr` 验证；实际执行结果和未覆盖范围以 [实验实现状态](implementation.md) 及交付报告为准。

## 2. 建议的调用职责

| 未来职责 | 调用入口 | 负责接口 |
| --- | --- | --- |
| `DsrClaimClient` | `worker.ts` 的容量与准入循环 | 1、2 |
| `DsrLeaseController` | register 后立即建立，同 epoch 重新注册共用一个恢复任务 | 3、4 |
| `DsrInputDispatcher` 与 delivery uploader | SSE 接收、task/control 两条处理队列 | 5、6 |
| `DsrInputFinalizer` | 当前输入完成及完成输入重投 | 7 |
| `DsrReplication` | 本地 Session 持久化、接管恢复与执行屏障 | 8、9、10、11 |
| `DsrToolApproval` | 全部工具审批及调用放弃 | 12、13 |
| `DsrPublicEvents` | 预览、完成块、工具结果和线程输出 | 14 |
| 现有各运行时客户端 | sandbox、MCP、web、附件、遥测 | 第 15 节，保持原生协议 |

不建议把这些状态机都塞进一个 `DsrClient`。可以共用 HTTP 传输、凭据、错误解析与路径构造，但租约、复制、输入结算与工具执行各自保留所有者。

## 3. 接口 1–7：领取、租约和输入

### 1. POST claims

当前调用者是 `claim.ts` 的 `BrainClaimClient.claim`，由 `claimTasks` 启动探测和有容量时的长轮询触发。未来仍由 host 的 claim client 调用，不放进 Agent runtime。

请求 `{}`、`block_ms`、fleet token 与 `X-Worker-ID` 均有现成来源，204 表示没有工作，响应字段足以构造领取记录。需要适配 `code_session_id`、`session_token` 与字符串 epoch；旧 `validateBrainTask` 会拒绝这份响应。`claim_expires_at` 应成为准入截止时间，而不是读取后丢弃。

判断：请求响应合理，需字段适配和十进制字符串校验。claim 不是幂等操作，丢失响应后消耗一次 attempt 是已接受限制，不能按普通写接口声称“重试返回首次结果”。

### 2. POST claims/{code_session_id}/release

当前 `BrainClaimClient.release`、`releaseClaim` 和 `worker.ts` 的退役/排空路径调用。未来覆盖准入失败、permanent、正常 idle、停机和新增审批挂起。fleet token、worker ID、领取 epoch 和 reason 均可取得。

请求 `{epoch,reason}`、响应 `{ok:true}` 足够，但 release 是“请求体不携带 epoch”的例外。必须固定重复释放的语义：建议同 owner、同 epoch 已释放也返回成功；被新 owner 领取则冲突，不得清除新租约。

关闭输入流并等待 reader 退出、停止 heartbeat、确认所有结果与复制队列后才能 release。独立清理预算应允许有界重试；模糊响应最终可由租约到期兜底，不能借新 epoch 重发旧 release。

判断：保留，补齐幂等和关闭顺序；挂起调用依赖新的 suspend 能力。

### 3. POST register

生产入口是 `worker-registration.ts` 的 `registerWorkerTask`，不是旧 `CcrClient.register`。未来首次准入函数和 lease controller 的同 epoch 重新注册共用 HTTP 逻辑。

请求 `{}` 合理。响应 epoch、protocol、lease、遥测配置和 context 均有消费者。根配置和子 Agent 模板在首次注册时冻结，重复注册与接管复用，并绑定当前 owner 凭证。注册成功后立即启动 heartbeat，然后构造 Agent、读取日志。

410 后同 epoch register 不得调用旧 `resetClaim` 清空队列、水位或等待项。多个并发请求同时收到 410 应共享一个重新注册任务，期间暂停新工具派发。首次重试受 claim deadline 限制。

判断：字段充分，调用所有权和恢复方式必须调整。

### 4. POST heartbeat

当前由 `ccrv2/plugin.ts` 的 `CcrBridge.createSession` 绑定 `CcrHeartbeat`。未来 lease controller 从 register 返回起负责整个 owner 生命周期，恢复日志、模型等待及审批期间都持续运行。

空请求和 `lease_expires_at` 响应足够。409 superseded 全树 fence；410 进入同 epoch 重新注册，成功后继续旧运行实例。不能沿用“任何 409/410 都永久失去 owner”的判断。排程与重试不得挤占下一拍；需要记录服务端到期时间，避免只凭本地时钟认定有效。

判断：保留，改错误分类与启动时机。

### 5. GET inputs/stream

当前 `CcrClient.inbound/readInbound`、`consumeInbound`、`CcrSession.receive/control`。未来 SSE client 解析传输，dispatcher 按显式 lane 分发，不再从 payload.type 猜测通道。

SSE 帧 id 是重连游标，data.id 是输入身份，二者不能混用；评论行只刷新 watchdog，不回执。task 串行、control 独立，原有系统上下文和 trace 字段保留。首次 received 异步发送，但 task 执行必须等待自己的 processing 成立。

两个阻断需要冻结前明确：unknown control 可 processed+error，但 unknown task 不能走同一路径，因为 task 禁止 error 且先要求 finish；task 的 session_thread_id 也不能默认支持任意子线程，因为当前 receive 总是执行根 runtime。建议 v1 task 仅主线程，子线程通过已有委派能力驱动，目标线程规则仍待冻结；本次明确未知 task 停止执行且不 ACK，按协议缺陷 permanent 退役，避免静默消费。

判断：信封合理，未知输入和目标线程规则必须补全。

### 6. POST inputs/delivery

当前 `CcrClient.reportDelivery/acknowledge`、inbound 自动 received、`CcrSession.receive` 首次 processing 及 `delivery-progress.ts` 周期续报。

请求 1–64 项合理。原响应只有 applied/ignored，批中存在 ignored 时不能定位哪条输入已 processing，当前客户端还会丢弃响应体。建议增加逐项 results：`{id,status,outcome,execution_status?}`。已知 task 的 processing 应独立于 Redis ACK 定位推进数据库 turn；ACK 定位只决定 InProgress/DoubleAck 是否执行。

Brain 必须确认对应结果的 execution_status 为 running 或 completed 后，才开始执行或走已完成补结算。不能把任意 200 等同于 processing 成立。processed 若 ignored，不可伪造确认成功或删除投递记录，留待重投补 ACK。

finish_required 要定位违规 input、补 finish 后重试原批，不得像 epoch_superseded 一样 fence。原文“一直重试”与通用有界重试冲突，需统一预算，耗尽不越过执行或 ACK 屏障。本次已在草案新增逐项结果 DTO 并标明这些要求；它是待实现的协议响应，不能按现有服务端已支持处理。

### 7. POST inputs/{input_id}/finish

未来从 `CcrSession.receive` 的 afterRun 以及已完成输入重投分支抽出 finalizer。顺序是写精确 completion → 本地与远端确认 → 已捕获预览与持久公开输出确认 → finish → processed。日志屏障必须覆盖 completion，不仅是 turn/end。

result/reason 有现成业务来源，但旧 `InputOutcome` 不能表达 cancelled，且依赖自由文本，必须升级本地插件记录。重试只能从持久记录恢复同一请求，不能按新进程遇到的错误重新选择 result。

响应的 public_status 不能承担调度：requires_action 是 idle stop reason，不是独立公开 status，并且并发终止会产生 terminated。本次将草案响应改为仅保留首次 result/reason，删除 public_status；公开状态由事件流提供。

判断：结束动作必要，completion 和响应 schema 需调整。错误时保持 input 未 ACK，不能为了清理队列换一种结果再试。

## 4. 接口 8–14：日志、审批和输出

### 8. PUT transcripts/{dsh_session_id}/header

当前 `CcrReplication.synchronize/persistRecord` 获取原生 Header；未来 `DsrReplication.ensureHeader` 由后台复制与 flushThrough 共用。主线程先确认，随后才允许其子 Header；子 Header 在公开 thread_created 前确认。

请求 Header、parent 已有来源，响应 next_seq 足够建立水位，但不证明内容前缀相同。应限制“首次远程 Event append 前确认 Header”，不能要求本地创建后、任何本地事件之前调用，因为原生初始化已可能 append。本次已修正时机。

判断：保留；conflict 停止并保留文件，版本不支持 permanent，429/5xx 同请求重试。

### 9. POST transcripts/{dsh_session_id}/events

当前复制器从本地 Session snapshot/handle 取增量，runtime 工具执行、公开输出和输入结算调用 checkpoint。未来每条原生日志独立有序队列，显式 `flushThrough(seq)` 与后台上传共用。

请求必须是落盘完成的连续 SessionEvent，不能照搬旧根会话混合 Header/Event 的队列。确认 seq X 的条件是 next_seq 大于 X。gap 补缺口，header_required 先补 Header，conflict 保留本地，purged 不重建。

413 应区分批次超 16 MiB 和单事件超 15 MiB：批次可以拆小重试，只有已写本地且单条不可复制时才 permanent。Header 重复返回的水位、错误头水位都不能用于跳过内容核对。本次已澄清大小错误规则和执行开始屏障。

判断：响应足够，拆队列与拆批是必做适配。文档原生事件缩略示意不能当完整 schema fixture，真实样本须由固定版本 DSH 产生。

### 10. GET transcripts

当前 `CcrSubagentBridge.restoreThreads` 合并本地列表和 privateEvents 的子线程发现。未来 register 后、任何 runtime resume 前列目录，即使本地根日志存在也要调用，避免漏掉其他 owner 创建的子日志。

ID、parent、公开线程绑定、header_present、next_seq 足够发现目录。配置从原生 descriptor 与 subagent-config 记录恢复，不由目录响应重建。

需处理 Header-only、未绑定公开线程的创建中断残留，不能把它们当作必有可运行 Agent。现有最大活跃子线程数不限制累计历史日志数；无分页响应存在无界增长，应增加稳定分页与恢复期间的完整枚举规则，或给出明确硬上限。

判断：必要，补分页和半创建状态规则后再冻结。

### 11. GET transcripts/{dsh_session_id}

当前 `CcrReplication.load/ensure/validateRemote`，未来逐页读取后继续使用临时 JSONL 完整校验，再做正式本地前缀对账。

Header、events、next_from_seq 已足够；首条 seq、连续性、Header ID/parent/version、游标严格前进都必须校验。from_seq=0 返回 Header 和 seq0，分页预算按原始 Header/Event JSON 字节累计，外层封装不计；单事件超软预算仍要返回一条。

已列出的日志若读取404，不可静默当作不存在并重建；410不可恢复。不同页的归档/还原不得改变逻辑事件序列。只读完整校验成功后才能修改正式文件。

判断：保留，补齐客户端一致性验证。

### 12. POST permission-requests

当前各工具通过 `approveToolCall` 到 `CcrSession.approve`，包括远程工具、AskUserQuestion、原生委派和 unavailable 工具。未来单一 approval owner 先登记稳定 waiter，再发送。

input ID、turn、Session/call ID 已存在，但当前 ToolApproval 不携带完整上下文，必须扩展。注册时保存 `{kind,name,server?}`；MCP name 应固定为远端工具名，server 固定为配置 server.name，模型别名另存映射。必须持久化身份，不能恢复时重新发现后再反解名字。

响应状态足够。external_result 应映射为 allowed:true 加 externalResult，而不是仅 behavior===allow 才允许。需要固定结果 content/isError schema、updated_input 的支持范围与 available 缺省归一化。恢复重试使用原始参数和首次身份，不能使用有效参数造成摘要不一致。

202后必须有相同 POST 查询终态的策略，处理丢失 permission.resolved；不能无限只等内存 waiter。哈希字节编码、base32规范与 TS/Go 共享样本仍需确定。

判断：保留；审批事实先于执行开始记录，全部有副作用路径在派发前经过屏障。

### 13. POST permission-requests/{request_id}/cancel

当前只有本地 reject/delete waiter，没有持久取消端点。未来由“明确放弃调用”触发，用户 interrupt 已由服务端close时可不调；挂起保留审批时不得调用。

请求 Session/call ID 足够派生墓碑，input_id可校验。返回当前终态能解决cancel与resolved竞争。若返回resolved allow/external_result，本地已放弃的调用仍不得执行。

需要独立于已 abort 工具 signal 的短清理预算，否则 cancel 无法发出。失去 epoch 后停止写入，不用 fleet token 绕过。取消先于创建的墓碑规则合理。

判断：保留，补清理 signal 和竞争后的不执行保证。

### 14. POST public-events

当前由 `CcrSession.record/replay/publish/publishPreview`、`CcrSubagentBridge.emit`、`CcrCompaction.flush` 产生。未来迁移到专用 publisher，移出审批、result和公开状态推导，保持捕获批次→日志确认→已有预览确认→持久输出的顺序。

工具结果、线程事件等字段有来源，但当前结果缓存只有 callId到name，需增加可恢复 ToolIdentity。线程 bridge 对非created事件使用randomUUID且缺原生message ID/seq，单改HTTP emitter无法稳定去重。应携带原生source identity，或先写完整公开输出插件记录再发。

助手消息另有关键差异：`ccrv2/assistant.ts` 已用 message ID/block index生成最终sevt ID，并保存model_request_start_id。服务端不能再派生一次不同公开ID。需明确模型块与preview使用相同最终ID，保留请求关联；普通系统/工具/线程事件才采用通用派生规则。本次已把这一要求列入草案，精确字段与校验仍需固定。

accepted/duplicates足够确认持久批次，但两类冲突必须区分：同一worker幂等键不同内容应409；模型代理echo冲突按现行保留首次并告警。preview不持久、不计数，与持久批次失败的原子范围要明确。

判断：接口必要，身份与双写字段尚不足以机械映射；实现前需真实请求响应fixture。

## 5. 第 15 节：逐个运行时路由

下表的 `S` 代表 `/v1/dsr/sessions/{code_session_id}`。这是建议的明确路径映射，不是已经存在的路由。DSR-Protocol与session token由面向OMA的transport注入，不得透传给外部MCP上游。

| 方法与新路径 | Brain 调用位置与时机 | 请求与响应评审 |
| --- | --- | --- |
| `POST S/sandbox/mcp` | `oma-sandbox/client.ts` 的 `SandboxMcp.provision/call`，沙箱工具按需初始化与执行 | 保留MCP JSON-RPC、协商、取消、JSON/SSE/通知响应；子线程保留X-OMA-Session-Thread-ID。必须保留X-Oma-Sandbox-Not-Forwarded，用于区分未派发和结果未知。tools/call不能套普通5xx自动重试 |
| `GET/POST/DELETE S/mcp?mcp_url=...` | `external-mcp/proxy.ts externalMcpFetch` 包装MCP SDK | GET为SSE、POST为RPC、DELETE清理MCP session；保留原始body和MCP headers。目标URL由后端快照和网络策略校验；不能把所有上游401/403当owner失效 |
| `GET/POST/DELETE S/mcp/{server_name}` | 同一fetch包装器，识别后端下发named URL | 服务端从配置选Tunnel目标。Go mcp_runtime_config.go的URL builder与Brain named prefix识别必须一起改，否则会重复包装root proxy。保留URL编码 |
| `GET /.well-known/oauth-protected-resource/v1/dsr/sessions/{code_session_id}/mcp/{server_name}` | MCP SDK授权发现，非Agent业务手写调用 | 需与named路由一起定义；WWW-Authenticate、resource、metadata URL均应改为新路径。读metadata是否需要认证必须延续发现流程，不能简单套全部DSR session路由的必带请求头规则 |
| `POST S/web/search` | `web-tools/client.ts WebClient.call` 的web_search | 请求query；响应sources含title/url/snippet/date?。策略403是工具拒绝，不fence；长结果保存沙箱的行为保持 |
| `POST S/web/fetch` | 同一client的web_fetch | 请求url；响应url/status_code/title/content/truncated及可选full_content。Brain会把full_content存文件，不能漏字段。保持服务端30秒与客户端35秒预算 |
| `GET S/threads/{thread_id}` | 旧CcrClient.getSubagentThread，子线程恢复前查询 | 响应id/status/archived，no-store；校验id与租户/会话归属。404表示该子线程不可用，不能误判根Code Session不存在 |
| `PUT S/attachments/image-attachments/{sha256}` | `RemoteAttachmentStore.saveImage` | body为图片bytes，Content-Type为MIME；200/201响应attachment_id/media_type/bytes。摘要、MIME、长度校验，幂等上传。不能套JSON stringify |
| `HEAD S/attachments/image-attachments/{sha256}` | `RemoteAttachmentStore.readImage` 每个assignment/epoch首次授权 | 无body，读取MIME/length/ETag；即使本地缓存命中也要授权。HEAD不能因通用JSON decoder报错 |
| `GET S/attachments/image-attachments/{sha256}` | readImage缓存不可用时下载 | 返回bytes，校验长度和SHA256后缓存；404为附件缺失，不代表会话丢失 |
| `GET S/attachments/message-attachments/{file_id}/content` | `RemoteAttachmentStore.source` 物化Files图片 | 流式原始文件与MIME/长度；按会话关联授权。非图片/超限/404按图片输入错误处理 |
| `POST S/diagnostics` | 未发现生产Brain调用方；workerDiagnostic写console并emitBrainLog | 原handler接收任意lines，不符合“白名单元数据”描述，也携带旧body epoch。建议v1不新增此未用端点；草案暂保留为待定项，需先明确调用方、typed schema和上限，不阻塞turn |
| `POST S/otlp/v1/logs` | `observability/index.ts BrainObservability.reporter` 注册OTLP exporter | 保留OTLP JSON/protobuf、gzip及原生响应；2秒export超时，不进入20秒通用JSON请求。register保留observability_enabled |
| `POST S/otlp/v1/traces` | `BrainObservability.ensureTraceOwner` 创建SpanExporter | 保留OTLP resourceSpans、int64字符串和trace/span ID编码；成功可能有partialSuccess。不得将OTLP状态体改成Anthropic错误。失败不阻塞工具或ACK |

MCP工具允许修改参数时，执行的是有效参数，permission重试仍提交原参数。OTLP成功但部分拒绝要计数并记录诊断；不能重发整批造成已接受span重复。

## 6. 模型和沙箱侧接口的调用归属

| 接口 | 调用方与请求 | 响应与保留要求 |
| --- | --- | --- |
| `POST /v1/messages` | `model-provider.ts omaRoute` 使用Anthropic AI SDK，model_token | 保留messages响应/SSE、Request-Id、模型message/block身份。子线程继续X-Claude-Code-Agent-Id，用途继续X-OMA-Model-Purpose；不套普通DSR重试和20秒timeout |
| `POST /v1/chat/completions` | 同一route选择OpenAI compatible SDK，model_token | 保留reasoning、usage、图片转换，max_tokens转max_completion_tokens，model streaming保持300秒idle timeout。模型用途为compaction/session-title时抑制公开输出 |
| `GET /v1/code/upstreamproxy/ca-cert` | 沙箱代理读取CA，Brain无直接调用 | 返回PEM且no-store；当前有意不鉴权，是“全部role=sandbox JWT”表述的例外 |
| `GET /v1/code/upstreamproxy/ws` | 沙箱relay，sandbox token | WebSocket/protobuf，CONNECT首包和沙箱归属校验保持。不得强加DSR-Protocol或session token |
| `POST /v1/code/sessions/{id}/sign-commit` | 沙箱git signing helper，contents/source?/git_object_format? | 响应signature；当前handler也接受有效worker credential，不能误写成现实现只接受sandbox。DSR迁移保持沙箱使用路径，Brain无新增调用点 |

模型代理的trace headers、请求ID与最终公开助手ID是接口17双写去重依据。仅路径不变但丢失这些metadata，仍属于不兼容。

## 7. 必须统一的跨接口规则

1. DSR控制与数据端点解析DSR-Error/DSR-Next-Seq；现有OmaApi.request抛出的HttpError只保留status，不能直接复用。错误对象应保留code、水位、Retry-After与安全请求ID，不打印原始body。
2. 普通4xx无DSR-Error的保守fence仅用于预期DSR控制端点。MCP上游拒绝、附件404、web策略拒绝和OTLP协议错误各自处理。网关生命周期拒绝仍以明确DSR-Error通知owner。
3. 重试必须区分幂等状态写入与工具执行。Header/events/permission/finish可按固定请求重试；MCP tools/call不可因5xx自动重放；claim丢响应也不能伪装幂等。
4. 同epoch重新注册是单例恢复，不重置input队列、复制水位或审批waiter；旧epoch回调不得取得新epoch凭据继续写。
5. 读接口虽允许旧epoch，不意味着旧owner能把读到的状态用于执行。所有工具、输出和ACK仍绑定当前owner。
6. 首次processing、远程日志确认、公开输出确认、finish和processed是不同确认，不能用单一HTTP200替代整条链。
7. 所有按原生事件重建的请求需要持久数据来源。工具身份、有效参数关联、completion result/reason和线程输出身份都不能只保留在内存。

## 8. 冻结前的逐接口验收

| 范围 | 必须补的验证 |
| --- | --- |
| 1–4 | 大epoch无精度丢失、claim响应丢失、release重复与新owner竞争、准入慢读期间心跳、并发410只有一次重新注册 |
| 5–7 | task/control乱序独立、unknown task策略、逐项processing确认、ACK定位丢失、finish丢响应、completion先复制、终态重投只结算 |
| 8–11 | 本地先append后写Header、分日志顺序、批次413拆分、单事件永久超限、目录分页、Header-only残留、跨归档分页和冷恢复校验 |
| 12–14 | TS/Go审批ID样本、202控制丢失后查询、external_result、有效参数重取、cancel独立signal、未知工具身份、模型双写ID相同、线程输出重启去重 |
| 运行时路由 | MCP全部方法及OAuth发现、取消和未派发头、附件四个方法、子线程权限、web全文保存、OTLP原生状态与partial success |
| 模型与沙箱 | 模型线程/purpose/request ID保持，sandbox token和CA例外保持，不将DSR头泄露到外部上游 |

当前测试为这些已有能力提供基线，新的DSR请求响应fixture和集成场景必须在实现后补齐。本次设计修改没有改变生产运行时。

输入执行模型的当前合同为 execution_status=accepted/running/completed、cancel_requested_at 与 finish.failure_code；本文早期 turn 评审术语以第 16 版协议与接口参考为准。
