# Open Managed Agents

[English](./README.md) | 简体中文

**在自己的基础设施上运行 Managed Agents，使用兼容 Anthropic SDK 的 API、隔离沙箱和可查看每次会话执行过程的控制台。**

[![Go 1.27.0](https://img.shields.io/badge/Go-1.27.0-00ADD8?logo=go)](./go.mod)
[![Bun](https://img.shields.io/badge/Runtime-Bun-000000?logo=bun)](./web/package.json)
[![Anthropic SDK compatible](https://img.shields.io/badge/Anthropic_SDK-compatible-765744)](./docs/en/quickstart.mdx)
[![Go unit tests](https://github.com/superduck-ai/open-managed-agents/actions/workflows/unit-tests.yml/badge.svg)](https://github.com/superduck-ai/open-managed-agents/actions/workflows/unit-tests.yml)

通过 API 定义 Agent、提交任务并订阅执行事件，OMA 负责执行调度、工具审批、文件持久化和会话恢复。你可以用它构建 Agent 应用，在本地运行整套服务，调试从模型调用到沙箱操作的完整链路。

![Managed Agents 控制台快速开始与 Agent 模板选择](./docs/images/console/managed-agents-quickstart.png)

_在控制台中选择模板，创建你的第一个 Agent。_

[快速开始](#快速开始) · [连接客户端](#连接客户端) · [架构详解](./OMA-Managed-Agents-Architecture.en.md) · [参与贡献](./CONTRIBUTING.md)

## 核心能力

- 通过 Anthropic SDK 连接自建 `/v1` 接口，管理 Agent、Environment、Session 和事件流。
- 通过 E2B 兼容接口在隔离沙箱中执行命令和文件操作，本地开发可使用 e2b-local。
- Brain 与 Sandbox 分离运行，持久化对话与执行状态，支持替换 Worker 后恢复会话。
- 通过 Filestore 和 FUSE 挂载上传文件、Skills、memory 和产物，按会话控制路径与权限。
- 通过平台代理管理上游凭证和工具审批，通过出站 Tunnel 连接私网 MCP 服务。
- 在 React 控制台中查看会话、工具调用、延迟与用量。

## 架构概览

Agent 定义行为与工具，Environment 定义执行条件，Session 承载一次任务，Events 传递输入、进度、审批与结果。

![OMA 架构：平台、Brain、Sandbox、状态存储、安全与文件系统](./docs/images/managed-agents-architecture/overall-architecture.jpg)

平台管理资源、权限和持久状态；Brain 运行 agent loop，编排模型与工具调用；Sandbox 执行命令并提供会话文件系统。外部 MCP 和 Web 工具使用各自的集成路径。会话恢复基于已保存的状态；结果不确定的工具调用需要核对后才能重放。

关于 CCRv2、凭证隔离、FUSE 和 MCP Tunnel 的设计，见英文文章 [OMA: From Running an Agent to Delivering a Reliable Service](./OMA-Managed-Agents-Architecture.en.md)。

## 快速开始

使用 Docker Compose 体验控制台、API 和本地沙箱基础设施，无需编译 Go 或前端代码。

需要 Git、[just](https://github.com/casey/just)、OpenSSL 和 Docker Compose。运行平台为 Linux Docker Engine 20.10+ 或 macOS 上的 OrbStack；沙箱网关需要 host 网络。请保留默认端口 `28080` 和 `38080`。

### 1. 获取源码

```bash
git clone https://github.com/superduck-ai/open-managed-agents.git
cd open-managed-agents
```

### 2. 初始化本地配置

```bash
just init-compose-config
```

该命令创建被 Git 忽略的 `deploy/docker-compose/oma-server.local.yaml` 和代理 CA 私钥，已有文件不会覆盖。默认配置用于本地开发，模型 Provider 凭证在服务启动后通过控制台添加。

### 3. 启动服务

```bash
docker pull ghcr.io/superduck-ai/managed-agent-sandbox:latest
docker compose up -d
```

首次启动需要下载镜像。检查服务就绪状态和 API 访问：

```bash
curl --fail http://localhost:38080/readyz
curl --fail http://localhost:38080/v1/models \
  -H 'X-Api-Key: sk-ant-local-default'
```

打开[控制台](http://localhost:28080)。首次调用模型前，在工作区的 **LLM 模型**页面配置 Provider 与模型 ID。客户端应使用同一工作区的 API key；`sk-ant-local-default` 属于默认 seed 创建的开发工作区。配置 Provider 前，模型列表可能为空。

将 `provider-slug/model-id` 替换为 `/v1/models` 返回的模型 ID，即可创建 Agent：

```bash
curl --fail-with-body http://localhost:38080/v1/agents \
  -H 'X-Api-Key: sk-ant-local-default' \
  -H 'anthropic-version: 2023-06-01' \
  -H 'anthropic-beta: managed-agents-2026-04-01' \
  -H 'Content-Type: application/json' \
  -d '{"name":"Hello OMA","model":"provider-slug/model-id","system":"You are a helpful assistant.","tools":[{"type":"agent_toolset_20260401"}]}'
```

成功响应包含 Agent 的 `id` 和 `version`。继续参考[首个会话教程](./docs/en/quickstart.mdx)，创建 Environment、发送任务并订阅响应事件。

启动异常时使用 `docker compose ps` 和 `docker compose logs --tail=100 oma-server` 查看状态。运行 `docker compose down` 停止服务，命名数据卷会保留。配置与生产部署要求见 [Compose 部署指南](./docs/design/docker-compose-deployment.md)。

## 连接客户端

将 Anthropic SDK 的 Base URL 指向 OMA，并使用工作区 API key。以下示例创建 Agent；任务执行使用 Session 及其 Events API。请安装包含 Managed Agents beta 资源的 SDK 版本。

先设置工作区中的模型 ID：

```bash
export OMA_MODEL_ID="provider-slug/model-id"
```

### Python

使用 `pip install anthropic` 安装 SDK，然后运行：

```python
import os
from anthropic import Anthropic

client = Anthropic(
    base_url="http://localhost:38080",
    api_key="sk-ant-local-default",
)

agent = client.beta.agents.create(
    name="Hello OMA",
    model=os.environ["OMA_MODEL_ID"],
    system="You are a helpful assistant.",
    tools=[{"type": "agent_toolset_20260401"}],
)
print(agent.id)
```

### TypeScript

使用 `bun add @anthropic-ai/sdk` 安装 SDK，然后通过 Bun 运行：

```typescript
import Anthropic from "@anthropic-ai/sdk";
import type { BetaManagedAgentsModel } from "@anthropic-ai/sdk/resources/beta";

const client = new Anthropic({
  baseURL: "http://localhost:38080",
  apiKey: "sk-ant-local-default",
});

const agent = await client.beta.agents.create({
  name: "Hello OMA",
  model: process.env.OMA_MODEL_ID as BetaManagedAgentsModel,
  system: "You are a helpful assistant.",
  tools: [{ type: "agent_toolset_20260401" }],
});
console.log(agent.id);
```

创建会话、发送输入和消费事件流的完整示例，见 [TypeScript smoke client](./tests/js-test/src/run-agent.ts) 和 [SDK 教程](./docs/en/quickstart.mdx)。

### Brain 运行时

`oma-brain-dsh/` 中的 Brain 运行 DeepSeek Harness，通过 CCRv2 与平台通信。平台管理运行时注册与会话凭证，配置方式见 [Brain 配置说明](./oma-brain-dsh/README.md)。

## 本地开发

修改源码和调试时，准备 Go `1.27.0`、Bun 及[开发指南](./docs/development-guide.md)中的工具。从仓库根目录执行：

```bash
just dev-init
```

首次使用时，在生成的 `~/.oma-dev/model-providers.yaml` 中填写 Provider 凭证，再执行一次 `just dev-init`。该命令会准备依赖、hook、密钥、开发数据和当前 worktree 的隔离基础设施。

从仓库根目录打开 Herdr，在控制分屏中执行以下命令，同时启动后端、Brain 和 Web：

```bash
just restart
```

单独重启服务可从仓库根目录运行 `just restart-server`、`just restart-brain` 或 `just restart-web`。访问地址、portless 设置与环境管理见[开发指南](./docs/development-guide.md)；手动启动、私钥、迁移与定向测试见[配置与运行指南](./docs/local-development.en.md)。

## 支持的 API

| 资源          | 端点                                        | 用途                   |
| ------------- | ------------------------------------------- | ---------------------- |
| Models        | `GET /v1/models`                            | 列出工作区已配置的模型 |
| Agents        | `/v1/agents`、`POST /v1/agents:search`      | 定义和查找 Agent       |
| Sessions      | `/v1/sessions`                              | 执行任务与交换事件     |
| Environments  | `/v1/environments`                          | 配置执行环境           |
| Deployments   | `/v1/deployments`、`/v1/deployment_runs`    | 管理部署与运行记录     |
| Files         | `/v1/files`                                 | 上传、下载与管理文件   |
| Memory        | `/v1/memory_stores`                         | 持久化 Agent memory    |
| Skills        | `/v1/skills`                                | 管理可复用的 Skills    |
| Messages      | `POST /v1/messages`、`/v1/messages/batches` | 模型调用与批处理       |
| Vaults        | `/v1/vaults`                                | 管理凭证资源           |
| Tunnels       | `/v1/tunnels`                               | 连接私网 MCP 服务      |
| Webhooks      | `/v1/webhooks`                              | 投递事件通知           |
| Organizations | `/v1/organizations`                         | 管理组织资源           |

SDK 请求使用 `X-Api-Key` 或 `Authorization: Bearer ...` 鉴权。Beta header 随资源类型变化，见 [API 参考](./docs/en/api/overview.mdx)和[版本说明](./docs/en/api/versioning-beta.mdx)。控制台路由使用 cookie session。

## 后续计划与参与贡献

后续计划包括 Agent 评估，以及基于执行轨迹改进提示词、工具和执行流程。背景见[架构文章](./OMA-Managed-Agents-Architecture.en.md#other-capabilities-and-plans)。

欢迎提交问题报告、SDK 兼容性修复、文档改进和运行时集成。开发准备、质量检查与设计约定见 [CONTRIBUTING.md](./CONTRIBUTING.md)，也可以[提交 Issue](https://github.com/superduck-ai/open-managed-agents/issues)。
