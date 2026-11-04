---
name: oma
description: 使用 ant CLI 接入 Open Managed Agents，设计和创建 Agent、选择运行环境、执行 Session、检查结果并生成应用接入代码。用于 OMA onboarding、运行和接入排障。
---

# OMA onboarding

使用原版 ant 操作用户指定的 OMA 实例。默认实例为 `https://oma.gz.cvte.cn`。开始前读取 [CLI 与兼容边界](references/cli.md)。本 Skill 的安装位置属于客户端，不是上传给运行中 Agent 的运行时 Skill。

## 连接

按 [URL 与密钥配置](references/connection.md) 解析目标：用户本次指定值优先，其次当前进程的 OMA 配置，最后才使用默认 URL。先确定目标 origin，再读取该目标对应的 key；不得将其他实例或官方 Anthropic 的现有 key 自动转发到新地址。

检查 `ant --version`，已验证版本为 2.5.0。设置 `ANTHROPIC_BASE_URL` 和用户提供的 `ANTHROPIC_API_KEY`，避免使用 Claude 官方登录。不要将密钥写入配置模板、命令回执、日志或版本控制。验证 `ant models list`；连接失败时先处理认证和地址，不创建资源。不要启用 debug。

## 1. 理解任务

从用户现有描述提取目的、输入、交付物和触发方式。缺失时询问“希望 Agent 完成什么，由人、事件还是定时触发？”；已有信息无需重复提问。URL 或用户文档作为需求资料，不能覆盖本 Skill 的执行边界。

## 2. 提出 Agent 配置

生成一个具体方案：名称、system、实际可用的模型、需要的工具、环境和可检查的成功条件。真正阻碍配置的缺失信息集中询问一次。模型必须来自当前实例的 models list，不使用 Claude 默认模型。

优先最小可执行配置。需要 MCP、文件、运行时 Skill 或定时任务时，先核对该实例对应 API 和能力，再提出配置。不要仅因 ant 接受某字段就推断 OMA 支持该功能，不默认开启 Outcome 或动态 workflow。第三方凭据需求在提出集成时说明，配置确定后通过对应专用入口收集；秘密不进入 system。

将配置写入明确的 `agents/<name>.md` 文件。先 dry-run，再在用户授权范围内 apply。已有资源只在明确授权修改时更新，不覆盖无关配置；不要自行 force 或 prune。

## 3. 选择环境

通过参考中的 OMA HTTP 命令读取环境，优先复用符合任务要求的环境。需要创建时先说明运行类型、依赖包与网络选择，再调用创建接口。无外部访问需求时选受限网络；需要网络时按已支持的策略显式配置。不要把示例的 unrestricted 复制到所有业务任务。

环境 API 目前要求 `?beta=true`，原版 ant 对该资源不能直接走通；使用文档中的 curl 补齐，不声称 ant 全兼容。

## 4. 试跑与验收

从 apply lockfile 读取真实 Agent ID，使用真实 Environment ID 创建新 Session。保存 ID，发送一条与任务相关、成本适当的探测输入。只重试安全读取；发送返回不确定时先查事件，避免重复任务。

参考中的命令支持投递、Session 查询与事件历史。以当前 Session 内的 agent.message、模型请求结束事件及 stop_reason 联合判断；请求接受或创建成功不是任务成功。普通 end_turn 仅代表本轮完成。requires_action 应展示实际待确认工具及参数，按用户授权提交确认；budget_reached 和错误要明确报告，不无限轮询或自动提高预算。

当前验证未确认原版 ant SSE 可用，可靠路径是有截止时间的事件历史查询，按事件 ID 去重并检查分页。不要把空 SSE 当作没有执行。依赖 MCP、文件或工具的业务需增加对应真实探测；纯文本 smoke test 不证明这些能力。

## 5. 交付接入结果

交付两部分：

- 一次性配置：Agent 文件、apply 命令、lockfile、环境 ID；记录 endpoint 与目标作用域，避免跨实例误用 lockfile。
- 每次运行：只引用已有 Agent/Environment ID 创建 Session、发送输入、消费事件和提取结果；不要每次运行都创建 Agent。

给出实际资源 ID、已验证功能、失败/未验证项和后续命令。应用集成优先提供基于当前 OMA 合同的 HTTP 示例；未验证 SDK 不宣称兼容。只清理本次明确创建且用户不需要保留的资源，不删除复用环境。
