# 本地开发环境使用指南

本指南用于在当前 checkout 或 worktree 中初始化开发数据，并通过 Herdr 在一个终端窗口中运行 oma-server、oma-brain-dsh 和 Web。对话运行使用 Brain 模式。

首次使用需要准备配置和密钥，再执行 `just dev-init`。日常开发只需在 Herdr 控制分屏中执行 `just restart`，不需要每天重新初始化。

## 准备工具和依赖

需要安装 Git、Go、Bun、just、OpenSSL、portless 和 Herdr。Go 版本以仓库 `go.mod` 为准。Docker 需要支持 Compose；本地 e2b-local 使用 host 网络，macOS 使用 OrbStack，Linux 使用 Docker Engine。

初始化会创建当前环境独立的 PostgreSQL、Redis、三节点 NATS、MinIO 和 e2b-local，无需另外手动启动共享 Compose 栈。

在仓库根目录安装依赖和 hook，并生成 Go 构建需要的文件：

```bash
just hooks-install
go mod download
(cd web && bun install --frozen-lockfile)
(cd oma-brain-dsh && bun install --frozen-lockfile)
./scripts/generate-go.sh
```

首次使用 portless 时，在普通终端执行：

```bash
sudo portless proxy start --no-tls
```

后续重启脚本会复用代理并注册当前环境的域名。

## 首次准备本地配置

本地配置、密钥和环境状态统一放在当前 worktree 的 `.run/` 中。该目录已被 Git 忽略，每个 worktree 独立准备。仓库的 `config/` 只提供配置模板。

### 创建配置目录

```bash
mkdir -p .run/secrets
chmod 700 .run .run/secrets
```

如果当前 worktree 没有基础配置，复制模板：

```bash
cp config/config.example.yaml .run/config.yaml
chmod 600 .run/config.yaml
```

已有配置时直接编辑，不要覆盖。`.run/config.yaml` 是生成环境配置的基础；`.run/generated/config.yaml` 会在初始化和重启时重新生成，不应手动修改。

### 准备固定密钥

首次在本机配置时执行：

```bash
just generate-code-session-jwt-key \
  ".run/secrets/code-session-jwt-ed25519.pem"

just generate-upstream-proxy-ca-key \
  ".run/secrets/upstream-proxy-ca-key.pem"
```

已有这两个文件时直接复用。生成脚本会拒绝覆盖已有文件。它们用于稳定的 JWT 签名和代理 CA，可以跨本机 worktree 复用。

基础配置还需要有效的 Vault 主密钥。若没有已有配置，首次生成本地 KEK 文件：

```bash
(umask 077; openssl rand -base64 -out ".run/secrets/vault-kek" 32)
```

此命令会覆盖目标文件，仅在文件不存在时执行。已有环境应保留原来的 Vault 主密钥，否则数据库内已加密的 provider 密钥等内容将无法解密。

在 `.run/config.yaml` 中合并以下配置；已有同名 YAML 节点时修改其字段，不要重复添加节点：

```yaml
environment_runner:
  agent_mode: brain

code_session:
  brain_claim_token_file: ./generated/secrets/brain-claim-token

vault:
  master_key:
    local:
      version: 1
      kek_file: ./secrets/vault-kek
```

使用 `kek_file` 时移除同一 `local` 节点中的示例 `kek` 字段。已有可用的 Vault 配置则保留，不必改为上面的文件形式。

`brain_claim_token_file` 的相对路径以 `.run/` 为基准。首次初始化会生成该文件；无需手动生成或填写 token。仅启动 Brain 进程不会自动把后端切换到 Brain 模式，必须同时设置 `agent_mode: brain`。

### 配置模型 provider

创建 `.run/model-providers.yaml`。下面是当前使用的 DashScope Anthropic 接口示例，模型使用 `kimi-k2.5`：

```yaml
version: 1
providers:
  - name: primary
    api_backend: messages
    base_url: https://coding.dashscope.aliyuncs.com/apps/anthropic
    api_key_file: ./secrets/dashscope-api-key
    model_ids:
      - kimi-k2.5
```

将真实 provider API key 单独保存到 `.run/secrets/dashscope-api-key`，文件只包含 key，并设置权限：

```bash
chmod 600 ".run/model-providers.yaml" \
  ".run/secrets/dashscope-api-key"
```

不要把真实 key 写入示例文件、提交记录或文档。`api_key_file` 的相对路径以 provider 配置文件所在目录为基准。

使用其他 provider 时，调整地址和模型列表。Anthropic 协议使用 `api_backend: messages`，OpenAI 兼容协议使用 `api_backend: chat_completions`。同一个 workspace 内，一个模型 ID 只能归属一个 provider；不要把 `kimi-k2.5` 同时配置到两个协议下。初始化不会访问上游自动发现模型，`model_ids` 必须显式填写。

### 配置初始化入口

复制模板：

```bash
cp config/dev-init.example.yaml ".run/dev-init.yaml"
chmod 600 ".run/dev-init.yaml"
```

编辑这份文件，重点修改：

| 字段 | 填写方式 |
| --- | --- |
| `fixed_secrets_dir` | 保持 `./secrets`，相对于 `.run/dev-init.yaml` 所在目录解析；也支持绝对路径 |
| `user.email` | 用于登录控制台的开发邮箱 |
| `organization.name` | 本地组织名称 |
| `model_providers_file` | 保持 `./model-providers.yaml`，引用同目录 provider 文件 |

保留模板中的两个 workspace、一个默认 workspace，以及每个 workspace 的两个 API key 定义。当前初始化合同要求这个数量，用户角色要求为 `admin`。`ref` 用于后续幂等匹配，不要随意改名。

## 初始化当前 worktree

所有 `just` 命令均在当前 worktree 的仓库根目录执行：

```bash
just dev-init
just slot-info
```

初始化会准备独立基础设施、应用数据库迁移、创建开发账号和两个 workspace、生成四个客户端 API key，并给两个 workspace 分别配置模型 provider。成功后 `slot-info` 中的 `init_state` 应为 `complete`。

环境名称由 worktree 路径派生，实际名称以 `just slot-info` 为准。不要手动指定编号，也不要从另一个 worktree 复制 `.run/env.json` 或 `.run/generated/`。

重复执行相同初始化文件会复用环境和已登记资源，不会清空数据库。初始化失败时先修正报错，再重新执行同一条命令；不需要先删除状态文件。

## 在一个终端中启动三个服务

在普通终端进入仓库并打开 Herdr：

```bash
herdr
```

在 Herdr 内的控制分屏中，进入同一个仓库根目录后执行：

```bash
just restart
```

首次运行会创建 `OMA services` 标签页：左侧为 oma-server，右上为 oma-brain，右下为 Web。后端监听成功后才启动 Brain，Brain ready 后再启动 Web。

再次执行 `just restart` 会复用当前 Herdr workspace 中属于这个 worktree 的服务分屏，只补建缺失分屏。请保留 `oma-server`、`oma-brain`、`web` 分屏名称，并从单独的控制分屏执行重启，不要在三个服务分屏自身运行。

成功后控制分屏会汇总输出可点击地址，例如：

```text
Open Managed Agents
  Web   http://oma-feature-x-web.localhost
  API   http://oma-feature-x-api.localhost
```

打开 Web 地址，使用初始化文件中的 `user.email` 登录。未配置 SMTP 的本地开发环境接受任意非空验证码；配置 SMTP 后应使用实际收到的验证码。

服务端口会动态选择，日常使用输出的 portless 域名。`just slot-info` 也可查看地址。绑定环境不要再通过 `PORT`、`API_PORT` 或手动设置 `OMA_BRAIN_CLAIM_TOKEN` 覆盖环境配置。

只重启某个服务时，可以在独立终端运行对应命令：

```bash
just restart-server
just restart-brain
just restart-web
```

这些命令都以前台方式运行，需分别占用一个终端。修改 `web/` 后，浏览器验证前需要重启 Web。

## 更新模型或凭证

修改私有 provider 文件或其 key 文件后执行：

```bash
just dev-init --update
```

`--update` 可应用 provider 地址、协议、模型列表和 key 的变化。普通初始化发现已有 provider 配置不同会报错，避免静默覆盖。

`just restart` 不同步 provider 数据。默认每个 worktree 维护自己的 provider 文件。如果显式引用外部共享文件，需要对希望更新的每个 worktree 分别执行初始化命令。已有 Agent 保存了模型配置，变更 provider 模型列表后也应检查 Agent 使用的模型。

修改 `.run/config.yaml` 中的 Brain 等运行配置后，在 Herdr 中执行 `just restart` 即可重新生成运行配置并重启。组织名称、用户邮箱和名称的变更当前不属于 `--update` 支持范围。

## 本地文件与清理

```text
.run/
  config.yaml           人工维护的基础运行配置
  dev-init.yaml         初始化定义
  model-providers.yaml  provider 定义
  secrets/              人工提供的密钥
  env.json              自动维护的环境绑定
  generated/            自动维护的运行配置、状态与凭证
```

| 文件 | 用途 |
| --- | --- |
| `.run/env.json` | 当前 worktree 的环境名称、身份和初始化文件位置 |
| `.run/generated/state.json` | 初始化阶段、资源 ID 与运行时端口 |
| `.run/generated/config.yaml` | 自动生成的服务配置 |
| `.run/generated/credentials.json` | 四个客户端 API key 和 Brain claim token 的明文，仅供本地保管 |
| `.run/generated/secrets/` | 复制的固定密钥与生成的 Brain token |
| `.run/config.yaml`、`.run/dev-init.yaml`、`.run/model-providers.yaml` | 人工维护的运行配置输入、初始化定义和 provider 定义 |
| `.run/secrets/` | 人工提供的固定私钥、Vault KEK 和 provider API key，清理环境时保留 |

初始化环境使用生成的客户端 API key，不要假定默认 key `sk-ant-local-default` 可用。provider API key 用于调用上游模型，与客户端 API key、Brain claim token 是不同凭证。

在各服务分屏按 Ctrl+C 可停止服务。关闭或 detach Herdr 客户端不代表服务已经停止。

确实需要删除本环境全部开发数据时，先停止三个服务，再执行：

```bash
just dev-teardown --yes
```

这会删除当前环境的 Compose 栈、数据卷、portless alias、注册记录和本地状态。`.run/config.yaml`、`.run/dev-init.yaml`、`.run/model-providers.yaml` 和人工提供的 `.run/secrets/` 仍保留。之后可用同一份初始化文件重新初始化，环境身份和客户端 API key 会重新生成。

## 从旧目录布局迁移

已有环境不需要删除数据库或重新生成凭证。迁移前停止三个服务，备份旧文件；不要对另一个 worktree 的文件执行这些操作。

1. 保留 `.run/env.json`。将旧 `config/config.yaml` 复制到 `.run/config.yaml`，将旧 `tmp/dev-init/` 的内容完整复制到尚不存在的 `.run/generated/`。
2. 从 `.run/env.json` 的 `init_file` 找到原初始化文件，将它与对应 provider 文件复制到 `.run/dev-init.yaml`、`.run/model-providers.yaml`。把人工密钥复制到 `.run/secrets/`，保留原值，并设置目录 `0700`、文件 `0600`。
3. 校正移动后的输入路径。初始化文件中使用 `fixed_secrets_dir: ./secrets`、`model_providers_file: ./model-providers.yaml`；基础配置中的 Brain token 使用 `./generated/secrets/brain-claim-token`。其他相对路径以各文件的新位置重新检查，尤其是 Vault KEK、JWT 私钥和 provider 的 `api_key_file`。
4. 将 `.run/generated/stack.env` 的 `OMA_E2B_CONFIG` 改为当前 worktree 下 `.run/generated/e2b-local.yaml` 的绝对路径。其余端口和环境身份保持原值。
5. 执行 `just dev-init`，让绑定和共享注册表登记新的初始化文件位置。确认环境 ID、客户端 key 和 Brain token 与备份一致。
6. 按当前环境名执行 Compose 协调，使 e2b-local 挂载新的配置位置。下面的 `feature-x` 必须替换为 `just slot-info` 显示的名字：

   ```bash
   docker compose -p oma-feature-x --project-directory "$PWD" \
     -f deploy/docker-compose/dev-env.yaml \
     --env-file .run/generated/stack.env up --detach
   ```

7. 在 Herdr 控制分屏运行 `just restart`。确认服务正常且 e2b-local 挂载已指向新路径后，删除本 worktree 中旧的 `config/config.yaml` 和 `tmp/dev-init/`。外部共享配置可能仍供其他 worktree 使用，不要一起删除。

如果迁移过程中失败，保留备份和原环境身份，先修正路径再重试。不要通过清空绑定或生成新 key 处理路径问题。

## 常见问题

| 现象 | 检查和处理 |
| --- | --- |
| 提示必须在 Herdr 内运行 | 普通终端先打开 `herdr`，再在其控制分屏运行 `just restart` |
| 分屏被关闭 | 在原控制分屏重新执行 `just restart`，缺失分屏会补建 |
| 提示无法识别分屏进程 | 检查是否在服务分屏中运行了其他程序，先手动退出该程序后重试 |
| 启动等待超时 | 查看对应服务分屏中的错误；确实只是编译较慢时，可用 `HERDR_DEV_START_TIMEOUT_MS=300000 just restart` 延长等待 |
| 提示初始化未完成 | 执行 `just slot-info`，修复初始化报错后重跑 `just dev-init` |
| portless 域名打不开 | 检查 proxy 是否以 `--no-tls` 启动，使用 `slot-info` 给出的完整 HTTP URL；首次启动代理可能需要 sudo |
| Brain ready，但消息一直排队 | 确认基础配置与生成配置均为 `agent_mode: brain`；仅有进程 ready 不代表对话执行已验证 |
| Brain 领取请求返回 401 | 重新执行 `just restart-brain`，让脚本读取当前环境 token；环境重建后旧进程持有的 token 已失效 |
| provider 修改后没生效 | 执行 `dev-init --update`，重启不会同步 provider；检查当前 workspace 和 Agent 的模型配置 |
| 容器、卷或网络归属校验失败 | 核对当前 worktree、环境 ID 和旧资源归属；旧版缺少身份标签的资源不会自动接管。确认归属后再处理旧资源，不要删除绑定文件来绕过检查 |
| 工具需要沙箱时报 E2B 错误 | Brain 模式下工具可能按需创建沙箱；还需检查 `e2b` 鉴权、模板和本地运行时配置，Brain ready 不等于沙箱已验证 |
| Go 构建提示缺少 Mapper | 执行 `./scripts/generate-go.sh`，完成后再重启；不要手改或提交生成文件 |

完整环境设计与恢复约定见[多 worktree 开发环境与初始化方案](design/development-worktree-environments.md)。
