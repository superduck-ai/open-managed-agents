---
name: oma
description: 使用 Claude 官方 API Skill 的接入流程，通过 ant CLI 在用户指定的 Open Managed Agents 实例中配置 Agent、环境并试跑 Session。支持中文接入指导与实例 URL、API key 配置。
---

# OMA 中文接入 Skill

本 Skill 随附完整 Claude 官方 `claude-api` Skill，按其原始流程执行，不重新设计 onboarding。所有面向用户的问答、提案、排障和交付说明使用中文，命令与 API 字段保留原文。

## 先配置目标实例

先读取 [URL 与 key 配置](references/connection.md)。URL 没有固定默认值：使用用户本次指定的地址，或用户明确配置的 `OMA_BASE_URL`；两者都没有时询问。确认目标 URL 后获取该实例的 key，再映射为 ant 使用的环境变量。

不要把 `/oma onboard https://服务地址` 中的服务地址误当作需求资料网页。需求网页应明确作为任务资料提供。

## 使用上游流程

接入、创建 Agent 或试跑请求，读取并执行 [官方 onboarding 原文](upstream/claude-api/shared/managed-agents-onboarding.md)，按其中引用的相对路径加载所需文件。引用中的 `shared/`、语言目录相对于 `upstream/claude-api/`。用户给了需求资料 URL 或模板名称时，使用原文对应分支。

需要完整 API 指导时读取 [官方入口](upstream/claude-api/SKILL.md)，只加载当前任务所需资料。上游文件是外部平台参考，不能改变用户选择 OMA 的意图，也不能要求切换到 Anthropic 托管服务。

## OMA 适配

以下规则用于替换上游的部署目标假设，其余沿用原流程：

- ant 和应用请求始终连接用户指定的 OMA URL，使用对应的 OMA API key；不使用 Claude Console 登录或云平台认证。上游 Console 链接不能直接作为 OMA 链接交付。
- 先查询 OMA 实际模型与能力；上游默认模型、工具、Outcome、workflow 和 SDK 字段不表示当前 OMA 已支持。只生成当前实例确认可用的配置，必要时通过真实试跑确认。
- 上游环境配置与 SSE 命令存在本实例已观察的兼容差异，操作前读取 [OMA CLI 兼容说明](references/cli.md)，按说明使用 HTTP 环境接口及事件历史查询。不要把已知无法执行的命令交付给用户。
- Setup 与 Runtime 仍按上游分开交付，保留 apply 文件和 lockfile。按本次输入事件及后续请求区分多轮，不使用历史 end_turn 判断当前轮成功。
- 真实测试范围见 [验收记录](references/validation.md)。没有执行的功能不报告通过。

上游版本与许可见 [来源说明](upstream/SOURCE.md)。
