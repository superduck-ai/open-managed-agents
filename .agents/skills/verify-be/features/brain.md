# Brain 验证

`just verify-be chat doctor brain-proxy` 和 `just verify-be chat brain-proxy` 专门验证沙箱代理在 owner 接管后继续工作，需要同样的 `OMA_BRAIN_BINARY` 与本地 Worker 镜像。该场景启动真实 Brain 进程，让脚本模型两次请求 Bash，使用自动继承的代理访问 `https://1.1.1.1/cdn-cgi/trace`。第一次成功后杀掉 owner，等待真实租约失效，再启动 replacement。必须观察 epoch 增长、同一个旧 worker token 在接管前后读取 session_context 均返回 200（`brain_proxy_old_token_identity`）、旧 worker token 调用沙箱网关返回 409 epoch 冲突、沙箱 UUID/provider ID/relay 地址全部不变，以及第二次 HTTP 200、CONNECT 200 和 TLS 校验成功。它使用隔离依赖和实际公网 HTTPS，不修改现存开发会话；公网不可达时不能报告通过。

运行前构建 oma-brain-dsh，设置 `OMA_BRAIN_BINARY` 为该可执行文件的绝对路径。

```sh
just verify-be chat doctor brain --worker-image oma/managed-agent-sandbox:latest
just verify-be chat brain --worker-image oma/managed-agent-sandbox:latest
just verify-be chat doctor brain-skills --worker-image oma/managed-agent-sandbox:latest
just verify-be chat brain-skills --worker-image oma/managed-agent-sandbox:latest
```

Doctor 检查 Brain binary 的可执行性并记录 SHA-256；沙箱镜像必须有
`/usr/local/bin/mcp-server`。新增的公开懒启动阶段需要 FUSE，仍不使用镜像中的 Claude 循环。

`brain_fencing` 使用 CCRv2 worker/register 验证跨租户拒绝和领取实例冲突，使用 sandbox/mcp 验证旧 epoch 凭据拒绝。
`brain_roundtrip` 同时启动两个 dsh 节点，实际消费 JetStream，核对公开 SSE preview、最终消息、
历史及单次模型请求。固定上游包含 thinking 和文本；SSE 与历史都必须只出现一次
`span.model_request_start → agent.thinking → agent.message → span.model_request_end`，且对应事件 ID 一致；thinking 仅保留状态，不得包含正文。
`brain_mcp_loop` 让真实原生循环请求 sandbox MCP Write，核对沙箱文件字节及下一次模型请求中的
工具结果。上游模型请求必须携带 `anthropic-version: 2023-06-01`，并且工具列表严格为
`Bash`、`Read`、`Write`、`Edit`、`Grep`、`Glob`、`web_search`、`web_fetch` 以及 dsh 内置的 `archive_agent`、`list_agents`、`send_to_agent`。
每次模型响应使用独立 message ID，避免不同回合被当作重复事件。任务通过 NATS 分发；输出、审批、状态和心跳复用 CCRv2 worker 接口，输入与回复通过 Worker SSE 接收，
processed 回执通过 CCRv2 delivery 接口确认。
`brain_takeover` 杀掉节点，先投递待恢复输入，再等待真实租约过期，重新派发任务并启动新节点，检查 epoch 增长、旧节点
MCP 请求被拒绝以及 CCRv2 私有 dsh_log 事件保存的原生模型历史可继续对话。
旧 `/internal/brain/` 接口不再参与验证；不需要 `OMA_BRAIN_TOKEN`。
执行权使用既有 Code Session 的 worker epoch、binding 和 60 秒租约，无独立 Brain 表。
`brain_retirement` 终止 Code Session，确认尚未过期的 owner 无法启动沙箱或调用 MCP。

`brain_lazy_denied` 通过公开 API 创建禁用工具的会话，核对工具拒绝后沙箱创建次数为零。
`brain_lazy_text` 通过公开 API 创建纯文本会话，核对回复与历史、沙箱创建次数为零。
`brain_lazy_tool` 在两个 Go Runner 与两个 dsh 节点下，核对首次获准工具只创建一个沙箱、
真实挂载和 MCP 写入，以及后续工具复用。随后删除原沙箱模拟 Provider 过期，核对再次 Write 创建且仅创建一个 replacement，并在新沙箱验证文件字节。
`brain_lazy_cleanup` 检查公开删除后，包含从未创建沙箱的会话在内的逻辑 Work 均为 stopped。

整个场景使用真实 Go 后端、PostgreSQL、Redis、三节点 NATS 和 Docker MCP；只模拟上游 LLM。
退出时终止所有 run-owned Brain 进程并删除 MCP 容器；源码保持不变。
不覆盖网络分区、云端 TTL、长 Bash、自动工具对账、真实模型或生产性能。

`chat brain-memory` 从旧 Crush 方案迁入 Memory 目录与加载回归。首次模型请求必须看到
已 attach store 的名称、路径、权限、description 和 instructions，但不包含正文。
拒绝 Read 和纯文本均不创建沙箱；首次获准 Read 经真实 MCP/FUSE 返回正文，后续调用复用。
公开历史关联、只读拒写及源字节保持都通过后产生 `brain_memory_loaded`。
六个基础 Memory 场景另见 [Memory](memory.md)。

`chat brain-skills` 使用公开 Skills API 上传 SKILL.md 和 reference，绑定 Agent 的 latest 引用，
启动两个 Runner、两个真实 Brain 节点。`brain_skills_catalog` 检查分层 system、Agent 原文、
名称/描述/固定版本和沙箱路径，并确认无 archive 正文、无沙箱创建。
随后上传新版本，旧会话通过真实 MCP Read 读取原版本 SKILL.md 和 reference，形成
`brain_skills_read`；实际只读 FUSE 挂载拒绝写入且原字节保持，形成 `brain_skills_readonly`。
新会话解析 latest 并读到新版本，形成 `brain_skills_snapshot`；公开删除与 Work 停止、
run-owned 节点和容器清理形成 `brain_skills_cleanup`。
上游模型脚本决定工具选择，因此此场景验证目录传递和加载路径，不验证真实模型的 Skill 选择能力。

## 压缩通知与恢复

`chat doctor brain-compaction` / `chat brain-compaction` 使用真实 DSH 可执行文件、公开 API、Runner、PostgreSQL、Redis、JetStream、MinIO 和沙箱 MCP。上游模型仍由 Go HTTP 服务提供固定响应，不验证真实摘要质量。

根会话与预定义子 Agent 分别运行：模型目录固定测试窗口 128000、输出上限 8192，先用长用户输入形成已提交历史，再在下一轮触发原生压力摘要，所有实际模型请求的输出预算必须不超过模型上限。子 Agent 场景的根模型使用大窗口以验证通知隔离；子实例通过真实 Write 写入文件并核对字节。

必需阶段为 `root_compaction_notified`、`root_compaction_recovered`、`child_mcp_verified`、`child_compaction_notified`、`child_compaction_recovered`。通知必须在公开 SSE 和历史中使用相同 ID，携带正的压缩前压力；原生 start/summary/end 必须已进入对应前台或子线程私有日志。随后 SIGKILL Brain，投递待执行消息，等待真实租约失效后启动 replacement，验证 epoch 增长、摘要标记和任务事实仍进入模型、原子线程继续执行，且无额外摘要或重复通知。

此场景不故障注入通知保存/发布失败，不验证崩溃发生在摘要中途、真实模型质量、云端分配或性能。发布失败重试由 Brain 单测覆盖。每次运行保持源码不变并检查进程、容器、volume 和临时配置清理。

## 真实视觉模型与图片

`chat brain-images` 是使用真实上游模型的显式场景。先设置 `OMA_BRAIN_BINARY`、`VERIFY_BE_VISION_PROVIDERS` 为私有 model-providers.yaml 的绝对路径、`VERIFY_BE_VISION_MODEL` 为其中唯一 provider 声明的视觉模型 ID，再运行 `chat doctor brain-images` 和 `chat brain-images`。不隐式读取共享模型配置；其他 chat 场景仍使用脚本模型。

场景将凭据只写入本次隔离 workspace 的加密 provider 记录。测试生成两个颜色不同的 PNG，提示词只列颜色候选，不透露答案。通过公开 user.message 发送第一张图，让真实模型回答颜色，形成 `message_image_seen`；第二张作为 File 挂载进 Session，模型必须通过真实 Worker 的 Read 读取并回答另一个颜色，形成 `read_image_seen`。最后检查私有原生 user/message 与 tool/result 均只保存 attachment 引用，并读取 MinIO 对象核对摘要和长度，形成 `native_images_persisted`。

使用公开 Session、生产 Runner、真实 DSH 进程、Docker Worker、FUSE 与隔离依赖。验证共享 fixture 的 CodeSession Service 也按生产组装注入附件 Filestore。不覆盖视觉准确率基准、计费、所有模型后端、跨实例恢复或云端沙箱。报告不能把此场景描述为脚本模型。临时配置、Brain 与 Worker/Compose 清理沿用现有 verify-be。

Brain doctor 同时检查 Worker 的 `environment-manager task-run --help` 声明 `--claude-agent-version` 与 `--claude-path`。旧测试镜像缺少这两个启动参数时提前拒绝，不能用 FUSE preflight 成功代替 Manager 启动兼容性。
