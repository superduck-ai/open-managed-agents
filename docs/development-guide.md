# 本地开发环境使用指南

本指南用于在当前 checkout 或 worktree 中初始化开发数据，并通过 Herdr 在一个终端窗口中运行 oma-server、oma-brain-dsh 和 Web。对话运行使用 Brain 模式。

LLM provider 定义和凭证在本机共享；运行配置、固定私钥、Vault KEK、环境身份和客户端 API key 按 worktree 隔离。首次只需填写一次共享 LLM 配置，后续 worktree 执行 `just dev-init` 即可准备环境。

## 安装必要工具

需要 Git、Go、Bun、just、portless 和 Herdr，Go 版本以仓库 `go.mod` 为准。Docker 需要支持 Compose；本地 e2b-local 使用 host 网络，macOS 使用 OrbStack，Linux 使用 Docker Engine。

首次使用 portless 时，在普通终端执行一次：

```bash
sudo portless proxy start --no-tls
```

后续重启脚本会复用代理并注册当前环境的域名。

## 用一条命令准备当前 worktree

在仓库根目录执行：

```bash
just dev-init
```

该命令统一完成：

1. 缺少 pre-commit hook 时安装仓库 hook，准备 Go、Web 和 Brain 依赖，并生成 Go Mapper。
2. 首次创建 `.run/config.yaml` 和 `.run/dev-init.yaml`，默认启用 Brain 模式。
3. 自动生成当前 worktree 的 JWT 签名私钥、代理 CA 私钥和 Vault KEK，文件保存在 `.run/secrets/`，权限为 `0600`。
4. 在本机共享目录缺少 provider 文件时，创建 `~/.oma-dev/model-providers.yaml` 模板。新 worktree 的初始化文件会引用它的绝对路径。
5. 启动当前环境独立的 PostgreSQL、Redis、三节点 NATS、MinIO 和 e2b-local，应用数据库迁移，创建开发账号、两个 workspace、四个客户端 API key，并分别写入共享 provider 定义。

已有配置、共享 provider 文件和密钥不会被覆盖或轮换。环境已绑定但人工输入配置丢失时会报错，要求恢复原文件，避免误生成新 Vault 主密钥导致旧数据无法解密。

首次使用这台机器时，还没有可用的 LLM 凭证。命令会先创建配置文件，然后在 provider 校验处提示缺少 key。按下一节填写共享文件，再执行同一条 `just dev-init` 即可继续，不会重复生成密钥。

## 本机只配置一次 LLM

编辑 `~/.oma-dev/model-providers.yaml`。下面是当前使用的 DashScope Anthropic 接口示例：

```yaml
version: 1
providers:
  - name: primary
    api_backend: messages
    base_url: https://coding.dashscope.aliyuncs.com/apps/anthropic
    api_key: "填写服务商提供的 API key"
    model_ids:
      - kimi-k2.5
```

直接填写 provider 的 `api_key`，并限制共享配置文件权限：

```bash
chmod 600 "$HOME/.oma-dev/model-providers.yaml"
```

模板的 `api_key` 默认为空，必须填写服务商提供的真实 key。共享目录只需保留 `model-providers.yaml`，不再需要共享的 `dev-init.yaml` 或 `secrets/`。真实 key 只保存在仓库外的私有配置中，不要写入仓库或文档。

Anthropic 协议使用 `api_backend: messages`，OpenAI 兼容协议使用 `api_backend: chat_completions`。根据上游修改地址和模型列表。同一个 workspace 内，一个模型 ID 只能归属一个 provider；不要把 `kimi-k2.5` 同时配置到两个协议下。初始化不会向上游自动发现模型，必须显式填写 `model_ids`。

所有新 worktree 的 `.run/dev-init.yaml` 都会包含类似下面的引用：

```yaml
fixed_secrets_dir: ./secrets
model_providers_file: /Users/你的用户名/.oma-dev/model-providers.yaml
```

共享目录不保存环境身份、Brain claim token 或客户端 API key。这些凭证仍由每个 worktree 单独生成。只有显式使用默认 `.run/dev-init.yaml` 的入口会自动准备本地文件；传入自定义初始化文件时，按该文件自行准备输入。

## 查看和调整初始化结果

```bash
just slot-info
```

成功后 `init_state` 应为 `complete`。环境名称从 worktree 路径派生，实际名称以命令输出为准。不要从另一个 worktree 复制 `.run/env.json` 或 `.run/generated/`。

默认登录账号为 `.run/dev-init.yaml` 中的 `user.email`，模板值是 `test@oma.ai`。需要自定义账号或组织时，在首次 provider 配置尚未完成、尚未创建开发数据前，编辑该初始化文件后重跑 `just dev-init`。组织名称、用户邮箱和名称目前不能通过 `--update` 修改。

重复初始化复用环境和已登记资源，不会清空数据库。失败后先修正报错，再重新执行同一条命令；不需要删除状态文件。`config/` 只提供模板，人工维护的本环境配置位于 `.run/`，自动生成的运行配置位于 `.run/generated/`。

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

再次执行 `just restart` 会安全停止并关闭当前 Herdr workspace 中属于这个 worktree 的旧服务分屏，然后新建三个分屏。新分屏等待 shell 就绪后才发送启动命令；全部启动成功后切换到服务页，失败原因和最近输出显示在控制分屏。请保留 `oma-server`、`oma-brain`、`web` 分屏名称，并从单独的控制分屏执行重启，不要在三个服务分屏自身运行。

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

修改共享 `~/.oma-dev/model-providers.yaml` 中的 provider 定义或 `api_key` 后，在每个希望更新的 worktree 执行：

```bash
just dev-init --update
```

`--update` 可应用 provider 地址、协议、模型列表和 key 的变化。普通初始化发现已有 provider 配置不同会报错，避免静默覆盖。

`just restart` 不同步 provider 数据。共享文件变化不会自动写入所有环境数据库，需要对希望更新的每个 worktree 分别执行上面的命令。已有 Agent 保存了模型配置，变更 provider 模型列表后也应检查 Agent 使用的模型。

修改 `.run/config.yaml` 中的 Brain 等运行配置后，在 Herdr 中执行 `just restart` 即可重新生成运行配置并重启。组织名称、用户邮箱和名称的变更当前不属于 `--update` 支持范围。

## 本地文件与清理

```text
.run/
  config.yaml           人工维护的基础运行配置
  dev-init.yaml         初始化定义
  secrets/              本环境固定私钥与 Vault KEK
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
| `.run/config.yaml`、`.run/dev-init.yaml` | 本环境运行配置输入和初始化定义 |
| `~/.oma-dev/model-providers.yaml` | 本机跨 worktree 共享的 LLM provider 配置与凭证 |
| `.run/secrets/` | 本环境的固定私钥和 Vault KEK，清理环境时保留 |

初始化环境使用生成的客户端 API key，不要假定默认 key `sk-ant-local-default` 可用。provider API key 用于调用上游模型，与客户端 API key、Brain claim token 是不同凭证。

在各服务分屏按 Ctrl+C 可停止服务。关闭或 detach Herdr 客户端不代表服务已经停止。

确实需要删除本环境全部开发数据时，先停止三个服务，再执行：

```bash
just dev-teardown --yes
```

这会删除当前环境的 Compose 栈、数据卷、portless alias、注册记录和本地状态。`.run/config.yaml`、`.run/dev-init.yaml` 和 `.run/secrets/` 仍保留，本机共享的 `~/.oma-dev/` 也不受影响。之后可用同一份初始化文件重新初始化，环境身份和客户端 API key 会重新生成。

## 删除 worktree 前清理开发环境

在目标 worktree 根目录执行：

```bash
just dev-recycle --yes
```

也可以从同一仓库的其他 checkout 中指定目标：

```bash
just dev-recycle --yes --worktree /absolute/path/to/worktree
```

命令先检查目标与环境归属，再停止该 worktree 的 oma-server、oma-brain-dsh 和 Web，等待进程退出；随后删除对应 Compose 容器、网络和数据卷，移除 portless alias、环境登记、`.run/env.json` 和 `.run/generated/`。无需进入 Herdr 分屏手动停止服务。未初始化或已经清理的 worktree 会跳过 Compose 清理，支持重复执行。

命令保留 worktree 目录、Git 登记、分支、源码和未提交改动，也保留 `.run/config.yaml`、`.run/dev-init.yaml`、`.run/secrets/` 与共享 `~/.oma-dev/model-providers.yaml`。主 checkout、锁定或有改动的 worktree 也可以清理开发环境，不要求先提交或 stash。

外部回收流程应先执行这个命令，只有退出码为 0 才继续删除 worktree。停止服务超过 30 秒或 Compose 清理失败时，命令报错并保留环境登记，排查后可重跑。不要同时运行初始化、重启和回收。Herdr 分屏会留下服务退出日志。

已经手动停止服务时，也可继续使用 `just dev-teardown --yes` 清理环境。已经被外部删除 worktree 的遗留资源，使用保留 checkout 中的 `just dev-teardown --orphans --yes` 清理。

## 从旧目录布局迁移

已有环境不需要删除数据库或重新生成凭证。迁移前停止三个服务，备份旧文件；不要对另一个 worktree 的文件执行这些操作。

1. 保留 `.run/env.json`。将旧 `config/config.yaml` 复制到 `.run/config.yaml`，将旧 `tmp/dev-init/` 的内容完整复制到尚不存在的 `.run/generated/`。
2. 从 `.run/env.json` 的 `init_file` 找到原初始化文件，将它复制到 `.run/dev-init.yaml`，将 provider 配置和原 key 值合并到 `~/.oma-dev/model-providers.yaml` 的 `api_key` 字段。把本环境固定私钥和 Vault KEK 复制到 `.run/secrets/`，保留原值，并设置目录 `0700`、文件 `0600`。
3. 校正移动后的输入路径。初始化文件中使用 `fixed_secrets_dir: ./secrets`，`model_providers_file` 指向本机 `~/.oma-dev/model-providers.yaml` 的真实绝对路径；基础配置中的 Brain token 使用 `./generated/secrets/brain-claim-token`。其他相对路径以各文件的新位置重新检查，尤其是 Vault KEK 和 JWT 私钥。
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

初始化会为每个 workspace 创建一个 `default` 云端执行环境，网络策略为 `unrestricted`。已初始化的 worktree 再运行 `just dev-init` 即可补齐；重复运行保留页面修改，已登记环境被删除或归档时会报错。
