# Brain 验证

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
`span.model_request_start → agent.thinking → agent.message → span.model_request_end`，且对应事件 ID 一致。
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
真实挂载和 MCP 写入，以及后续工具复用。
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
