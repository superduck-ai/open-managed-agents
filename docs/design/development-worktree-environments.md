# 多 worktree 开发环境与初始化方案

状态：已实现。初始化 CLI 位于 `cmd/dev-init`（`just dev-init` / `just slot-info`），
核心逻辑在 `internal/devinit`。本文描述当前行为、恢复约定与验收边界。

开发者操作步骤见[本地开发环境使用指南](../development-guide.md)，包括首次初始化、Brain 模式配置和 Herdr 日常重启。

## 目标

每个 worktree 绑定一个固定的本地开发环境，隔离任务领取、数据库迁移、消息、对象存储、登录状态和监听端口。通过一条命令初始化开发账号、组织、两个 workspace、四个 API key，以及从文件读取的 model providers。

每个环境使用独立 Compose 栈，包含 PostgreSQL、Redis、三节点 NATS、MinIO 和 e2b-local。共享 Docker daemon，但网络和卷按项目隔离。外部服务账号的额度和账号级资源仍然共享。

## 命令与日常使用

仓库跟踪 `.codex/environments/environment.toml`，将 Codex 本地环境的 setup
配置为 `just dev-init`，cleanup 配置为 `just dev-recycle --yes`。使用该本地环境
创建新 worktree 时自动初始化，归档并清理 worktree 时先回收开发环境。
回收失败时保留 worktree 和环境登记，修复后重试；手动通过 Git 创建或删除
worktree 时仍需显式执行对应命令。`.codex/` 下的其他本地文件继续忽略。

本环境配置统一使用 `.run/`，镜像前缀、LLM provider 配置和凭证常驻本机 `~/.oma-dev/`，
各 worktree 的初始化文件显式引用同一份 provider 定义。`config/` 中的配置文件
仅作为模板，开发初始化与重启不再读取旧的 `config/config.yaml`。
开发模板将 `code_session.upstream_proxy_disable_ssrf_protection` 设为 `true`；首次初始化复制该值，已有 `.run/config.yaml` 不会被覆盖。

```text
.run/
  config.yaml
  dev-init.yaml
  secrets/
  env.json
  generated/
    config.yaml
    state.json
    credentials.json
    stack.env
    e2b-local.yaml
    secrets/
```

`just dev-init` 通过 `scripts/dev-init.sh` 统一准备 hook、依赖与 Go 生成文件，
默认读取 `.run/dev-init.yaml`，也允许显式指定初始化文件。默认入口在输入缺失时
自动生成本环境配置、JWT 和 CA 私钥与 Vault KEK；配置默认启用 Brain，
新建初始化文件的 `model_providers_file` 指向 `~/.oma-dev/model-providers.yaml`
的绝对路径。共享文件缺失时从模板创建，用户首次填写模型和凭证后重跑同一命令。
已有配置、共享文件与密钥不覆盖；已绑定环境缺少输入时拒绝重新生成密钥。

首次生成的本地 `.run/config.yaml` 设置 `code_session.upstream_proxy_disable_ssrf_protection: true`，允许 Clash/TUN 将公网域名解析为 `198.18.0.0/15` Fake-IP 时使用沙箱代理。该设置仅属于 dev-init 的本地配置准备，放开代理拨号层的私网地址过滤，仍保留租户鉴权和 Environment 网络策略。通用配置模板与生产默认继续启用 SSRF 防护。已有 `.run/config.yaml` 不自动覆盖，使用 Fake-IP 的现有环境需显式设置该项并重启后端。

共享文件采用同目录临时文件与不可覆盖的硬链接发布，多个 worktree 同时准备时
只保留第一个完整文件。本环境准备与初始化分别使用文件锁防止重复写入。
配置准备只针对默认初始化文件，自定义初始化文件保留显式准备输入的行为。

`fixed_secrets_dir` 和 `model_providers_file` 支持相对于初始化文件目录的路径；
本环境固定密钥放在 `.run/secrets/`，生成密钥副本和 Brain token 放在
`.run/generated/secrets/`。基础配置中的相对路径以 `.run/config.yaml` 的目录解析。
共享 provider 凭证直接写在 `~/.oma-dev/model-providers.yaml` 的 `api_key` 字段中，
该文件权限为 `0600`；准备入口不再创建共享 `secrets/` 目录。共享定义更新后，各环境需分别执行
`just dev-init --update`，日常重启不会同步数据库中的 provider。

```mermaid
flowchart LR
    Base[.run/config.yaml] --> Generate[生成运行配置]
    Init[.run/dev-init.yaml] --> Generate
    Inputs[.run/secrets] --> Init
    Shared[~/.oma-dev/model-providers.yaml 与凭证] --> Init
    State[.run/generated/state.json] --> Generate
    Generate --> Runtime[.run/generated/config.yaml]
    Runtime --> Services[后端 Brain Web]
```

清理环境仅删除 `.run/env.json` 和 `.run/generated/`，保留人工配置与
`.run/secrets/`。这样清理后可以直接重新初始化，不需要重新填写 provider 凭证。
旧布局不自动覆盖迁移，升级时需停止服务并保留环境身份、凭证与输入配置，
再调整路径；操作步骤见开发指南的旧布局迁移部分。

入口如下：

```bash
just dev-init
just slot-info

just restart-server
just restart-web
just restart-brain
```

在 Herdr 终端内执行 `just restart`，可在同一个终端窗口中重启三个服务。
`scripts/restart-dev.sh` 每次在当前 Herdr workspace 中按服务名称和 worktree
工作目录查找旧服务分屏，确认是本环境服务后发送 Ctrl+C，等待最多 30 秒返回
shell，全部停止后关闭这些旧分屏。未知程序和其他 worktree 的分屏不会被中断
或关闭。从服务分屏自身执行会被拒绝，应在独立控制分屏执行。

随后新建 `OMA services` 标签页：后端位于左侧，Brain 和 Web 位于右侧上下分屏。
每个新分屏最多等待 30 秒进入空闲 shell；初始化期间出现临时前台进程只等待，
不发送中断，也不提前写入启动命令。脚本先等待后端输出监听成功，再启动 Brain，
等待 Brain ready 后启动 Web，每次使用唯一就绪标记。各服务仍调用现有
`restart-*` 入口，使用当前 worktree 的配置和进程归属检查。

启动全部成功后才切换到服务标签页；失败时控制分屏显示服务名、panel ID、
失败阶段及该分屏最近 30 行输出，停止后续启动并保留新分屏。脚本回归入口为
`bash scripts/tests/restart-dev_test.sh`，通过模拟 Herdr 验证等待、替换、拒绝误关及
失败诊断，不操作开发者的真实分屏。

三个服务就绪后，脚本汇总输出当前环境的 Web 与 API 完整 HTTP URL。
交互终端使用 OSC 8 超链接，重定向输出或 `TERM=dumb` 时使用纯文本 URL。

每个服务的启动等待默认最多 180 秒，可通过 `HERDR_DEV_START_TIMEOUT_MS` 调整。
超时或命令失败时停止后续启动，保留分屏供排查；已经启动的服务继续运行。
脚本依赖 Herdr、just 和 Bun，必须在 Herdr 内运行。退出或 detach Herdr 客户端
不等于停止服务；在对应分屏按 Ctrl+C 停止服务。

| 命令                       | 职责                                               |
| -------------------------- | -------------------------------------------------- |
| `dev-init [FILE]`          | 默认使用 `.run/dev-init.yaml`，初始化配置、依赖资源、固定密钥和开发数据 |
| `dev-init [FILE] --update` | 显式更新已登记资源的可修改配置                     |
| `slot-info`                | 显示环境名称、访问地址、资源名称和初始化状态       |
| `restart-server`           | 使用当前环境配置重启后端                           |
| `restart-web`              | 使用当前环境的前端端口和后端代理目标               |
| `restart-brain`            | 使用当前环境的 API 地址和 Brain claim token        |

日常重启只读取已准备好的配置，不同步开发数据、不轮换密钥，也不覆盖页面修改。未启用环境初始化的 worktree 保留现有运行方式。

## 环境名称与归属

环境名称从 worktree 路径派生，保存在 `.run/env.json`，绑定后保持稳定。
主 checkout 使用仓库目录名，linked worktree 使用区分该 checkout 的父目录名。
同 clone 内重名由注册表追加后缀。资源身份同时包含独立的 env_id。

- 重复初始化复用同一身份，未完成登记也不会重新分配名字或 env_id。
- 目录移动后用当前真实路径刷新本地绑定和公共 Git 目录中的注册表。
- 旧路径仍存在时拒绝把同一绑定迁移到另一路径，避免复制绑定后接管活跃环境。
- 跨 clone 同名会触发 Docker 资源身份冲突，不能自动复用或删除对方资源。
- 初始化失败保留绑定和恢复状态；三个启动入口拒绝未完成环境。
- `just dev-teardown --yes` 回收本环境；`--orphans --yes` 回收路径已不存在的环境。

## 资源与地址隔离

以名字 `feature-x` 为例：

| 项目 | 配置 |
| --- | --- |
| Compose 项目 | `oma-feature-x` |
| 后端监听地址 | loopback 动态端口，记录在 state.json runtime |
| 前端监听地址 | loopback 动态端口，记录在 state.json runtime |
| 后端稳定地址 | `http://oma-feature-x-api.localhost` |
| 前端稳定地址 | `http://oma-feature-x-web.localhost` |
| Vite `VITE_API_PROXY_TARGET` | runtime API 端口存在时直连 `http://127.0.0.1:<api_port>`，避免 portless 二次代理回环 |
| DSH `OMA_API_URL` | portless 发现的后端名字地址 |
| PostgreSQL、Redis、NATS、MinIO | 本环境 Compose 栈中的独立实例与卷 |
| 数据库和 bucket | 沿用基底配置名称，各栈内部独立 |
| Brain claim token、客户端 API key | 本环境独立随机生成 |

运行端口由初始化和重启入口探测，不从编号计算。初始化重跑保留已有 API/web
端口；重启先停当前 worktree 旧实例，再探测新端口。基础设施端口以已运行且
归属正确的容器发布信息为准，健康栈复用，异常栈由 Compose 协调恢复。

独立数据库隔离 DSH、Runner、River 后台任务和 goose 迁移版本。任务领取 SQL
不增加环境标签，也不引入 worker pool。

## 前端、后端与 DSH 地址一致性

三个启动入口消费同一份已解析环境配置，不能分别推导端口。

```mermaid
flowchart TD
    Binding[当前 worktree 环境绑定] --> Config[统一解析的环境配置]
    Config --> Server[后端监听地址]
    Config --> Web[前端端口与 Vite 代理目标]
    Config --> Brain[DSH API 地址与 claim token]
    Config --> Resources[数据库 Redis bucket NATS]
```

启用环境隔离后：

- 只停止已记录且属于当前 worktree 的实例，再探测新端口；记录中的端口被其他 worktree 或进程占用时拒绝停止对方。
- 重启脚本只停止确认属于当前 worktree 的服务；无法确认归属时明确报错。
- 临时 `PORT`、`API_PORT`、`OMA_API_URL` 等值与绑定配置冲突时，报错，不静默选择其中一个。
- DSH 显式接收当前环境的 API 地址和 claim token，避免 `.env` 中的默认地址覆盖它们。
- 如果配置了 diagnostics 固定监听端口，也需要单独检查冲突。
- 重启后端保留先完成 Go 代码生成、再停止旧服务的顺序。

未启用环境隔离时保留现有脚本语义，包括前端遇到其他 checkout 占用端口时顺延。

浏览器通过 `oma-<name>-web.localhost` 访问，隔离各环境 Cookie。portless 使用纯 HTTP；首次需要 `sudo portless proxy start --no-tls`。未安装 portless 的绑定环境拒绝启动。

前端继续通过现有相对路径和 Vite 代理访问后端，不改变 `/api`、`/v1`、`/auth`、`/oauth`、`/web-api` 的 API 语义。

`tunnel.public_base_url` 可以使用生成的本地地址，但远端 sandbox 或容器需要访问时，必须显式配置其可达地址。不能对所有运行模式强制使用 `127.0.0.1`。

## 本机镜像前缀

`~/.oma-dev/images.yaml` 是所有 worktree 共用的本机镜像配置，权限为 `0600`。默认入口从 `config/images.example.yaml` 补齐缺失文件，已有内容不覆盖，包括已经准备好 `.run/` 的环境。

```yaml
prefix: ghcr.io/superduck-ai
```

`prefix` 是 registry 与可选 namespace，不包含协议、凭据、镜像名或 tag。首尾空白及末尾 `/` 在读取边界规范化，空值或文件缺失时使用公开默认前缀。非法输入、未知字段、符号链接和多份 YAML 文档会被拒绝。

Compose 的 init-envd 与 e2b-local 使用生成的 `stack.env` 中的 `OMA_E2B_LOCAL_IMAGE=<prefix>/e2b-local:latest`。默认沙箱模板使用 `<prefix>/managed-agent-sandbox:latest`。空模板、`managed-agent-sandbox`、旧的 `oma/managed-agent-sandbox:latest` 与公开默认镜像会按本机前缀解析；显式自定义模板保持原值。PostgreSQL、Redis、NATS、MinIO 等基础设施镜像不受这个应用镜像前缀影响。

私有镜像地址只写入本机 `images.yaml`、忽略的 `.run/generated/` 及对应数据库资源，不进入源码、测试或文档。前缀变更后执行 `just dev-init --update`：既有默认模板会迁移到新配置，生成状态记录上次管理的默认模板引用，只有数据库值仍与该引用一致或仍为已知旧默认模板时才更新；页面设置的自定义模板保留。镜像需要预先构建或拉取；初始化不自动推送镜像。复用健康栈时也检查生成的 stack.env，镜像引用变更会重建当前环境的网关；失败时恢复原网关配置与 stack.env，允许重试。

普通单测覆盖公开默认值、私有前缀映射、自定义模板保留、输入校验、配置权限与重复准备，以及只变更镜像引用时的网关刷新与失败重试。

## 本地沙箱配置

绑定环境的生成配置统一连接本环境 e2b-local，并启用 `e2b.local_port_lookup`。`e2b.local_service_host` 固定为 `127.0.0.1`，使后端通过宿主机回环地址访问 Docker 发布端口，不依赖网关返回的虚拟机或局域网 IP。空 `e2b.api_key` 使用符合 SDK `e2b_` 加十六进制格式要求的本地占位值 `e2b_0000000000000000000000000000000000000000`，用于通过 SDK 创建请求前的凭据校验；显式 API key 保留。空模板或云端默认模板 `managed-agent-sandbox` 映射为本机镜像前缀下的 `managed-agent-sandbox:latest`，自定义镜像引用保留。初始化前应构建或拉取该镜像。

`code_session.sandbox_api_base_url` 生成为 `http://host.docker.internal:<当前 API 端口>`，每次重启后随动态端口更新。该地址用于本地容器回调宿主机 OMA，需要 Docker 提供 host.docker.internal 解析。

生成 e2b-local 配置时读取沙箱镜像的 Docker architecture，选择对应的 `envd-linux-amd64` 或 `envd-linux-arm64`。镜像不存在或架构不支持时初始化报错。init-envd 逐个补齐缺失的 envd 文件。复用健康栈时仍重新生成配置，配置变化则重建本环境的 e2b-local 并等待健康状态。

已有 workspace 的默认环境可能仍保存旧的 `managed-agent-sandbox` 模板。运行 `just dev-init --update` 会在 Yourbatis 事务中锁定已登记的默认环境，将仍使用已知默认模板且没有 build job 的 E2B 环境更新为当前本地模板。普通重复初始化和自定义模板保持原值。

普通单测覆盖生成配置中的凭据、模板、端口解析开关和回调地址，以及 envd 架构替换；生成凭据通过真实 SDK 创建入口请求进程内 HTTP 服务，防止仅检查非空却遗漏 SDK key 格式限制。真实沙箱创建与回调需要单独进行集成验收。

## 初始化后的开发数据

每个 workspace 初始化一个名为 `default` 的云端执行环境，使用当前 E2B 模板、空软件包列表与 `networking.type: unrestricted`，允许不受限的外网访问。环境 UUID 和 external ID 在创建前写入恢复状态；失败重试复用相同身份。已有 worktree 再运行 `just dev-init` 也会补齐该环境。重复初始化不覆盖页面修改，已登记环境被删除或归档时报错，不静默恢复。

```text
组织：OMA Local
└── 用户：test@oma.ai，组织管理员
    ├── default，默认 workspace
    │   ├── default-key-1
    │   ├── default-key-2
    │   └── model providers
    └── secondary，普通 workspace
        ├── secondary-key-1
        ├── secondary-key-2
        └── model providers
```

- `test@oma.ai` 可以访问两个 workspace，并作为四个 API key 的创建人。
- 普通 workspace 建立对应成员关系；默认 workspace 遵循现有权限规则。
- 新环境只有上述测试账号和四个初始化 API key，不额外创建 `admin@example.local` 或第五个默认 key。
- 登录继续沿用现有邮箱验证码流程，不增加密码登录或新的鉴权绕过。
- 已有数据库不自动删除历史账号、workspace 或 key；无法安全复用时报告冲突。

当前基础 seed 会创建并更新默认用户和资源。启用开发初始化模式后，必须调整其调用关系，使其与文件初始化使用一致的数据来源，防止后续重启把测试用户改回原默认账号。未启用此模式时保留原行为。

## 初始化文件

开发数据通过一个入口文件描述，provider 定义引用独立文件，便于各 worktree 共用。

`dev-init.yaml` 示例：

```yaml
version: 1

fixed_secrets_dir: ./secrets

organization:
  name: OMA Local

user:
  email: test@oma.ai
  name: Local Test
  role: admin

workspaces:
  - ref: default
    name: default
    is_default: true
    api_keys:
      - ref: key-1
        name: default-key-1
      - ref: key-2
        name: default-key-2

  - ref: secondary
    name: secondary
    is_default: false
    api_keys:
      - ref: key-1
        name: secondary-key-1
      - ref: key-2
        name: secondary-key-2

model_providers_file: /home/you/.oma-dev/model-providers.yaml
```

`ref` 是初始化配置内的稳定引用，与显示名称分开。API key 的引用在所属 workspace 内唯一。数据库资源继续使用 UUID 和现有 external ID 机制，引用与实际 ID 的映射保存在环境状态中。

组织初始化不假设现有数据库具备组织 external ID 字段，也不按名称自动接管无归属记录。新环境创建后登记组织 UUID；已有默认组织只能经过明确的归属校验后复用。

所有相对文件路径按声明它的配置文件目录解析。严格校验未知字段、重复引用、缺失密钥、默认 workspace 数量以及资源归属。

第一版开发配置要求一个测试用户、一个组织、两个 workspace，并且每个 workspace 声明两个 API key。

## Model provider 文件

`model-providers.yaml` 示例：

```yaml
version: 1

providers:
  - name: primary
    slug: primary
    api_backend: messages
    base_url: https://api.anthropic.com
    api_key: "填写服务商提供的 API key"
    model_ids:
      - your-model-id

  - name: compatible
    slug: compatible
    api_backend: chat_completions
    base_url: https://provider.example
    api_key: "填写服务商提供的 API key"
    model_ids:
      - another-model-id
```

- 支持现有 `messages` 和 `chat_completions` backend。
- 默认开发配置直接使用 `api_key`。解析器仍支持 `api_key_env` 和 `api_key_file`，三者必须三选一。
- 两个 workspace 分别创建 provider 记录，使用同一组文件定义；作用域保持 workspace 专属。
- 文件中的 `slug` 是新建记录的候选值，省略时使用 `default`。分配时检查整个组织（含组织级及其他 workspace 的连接），冲突时依次尝试 `-2`、`-3` 等后缀，并将结果限制在 32 字符内。例如同一个 `compatible` 定义在两个 workspace 中可以分别得到 `compatible` 和 `compatible-2`；模型引用使用数据库中的实际 slug。
- 重跑优先按状态登记的 external ID 查找，保留实际 slug，不重分配或改写模型引用。旧状态没有 `input_slug` 时，首次重跑沿用数据库身份和迁移后的 slug，并登记当前文件的候选值；之后修改该值会报错，包括 `--update`。已登记记录删除后报错，不按同名连接替换。
- 创建已提交但 provider 阶段状态尚未保存时，仅按所属 workspace 内唯一的名称恢复身份；同名多条记录时报错，避免接管错误连接。已有登记不受同名显示名称影响。
- `--update` 继续更新原记录的地址、协议、模型列表与密钥，保留 UUID、external ID、slug 和 workspace；未指定时拒绝覆盖差异。
- provider 密钥经现有 secrets 服务加密后写入当前环境数据库。
- 模型列表以文件为准，初始化时不自动访问上游同步模型。
- 保留现有 URL 校验和模型数量限制；不同 provider 可使用相同上游模型 ID，通过 `<slug>/<model_id>` 区分。
- 显式 `slug` 遵守现有格式校验且在文件内唯一。
- 文件中的 provider 名称作为所属 workspace 内的初始化引用；修改名称视为新增，旧记录不自动删除。
- 密钥是否一致不能通过 last4 判断，应通过现有 secrets 服务解密后在内存比较。

现有 HTTP handler 中的校验和密钥封装应整理到共享业务层，由页面操作和初始化共同调用，避免维护两套业务规则。

## 固定 JWT 与上游代理 CA 密钥

MITM 默认开启。`just dev-init` 首次生成 `.run/config.yaml` 时显式写入 `code_session.upstream_proxy_mitm_enabled: true` 并绑定 `.run/secrets/upstream-proxy-ca-key.pem`，生成配置沿用该开关并指向 `.run/generated/secrets/` 中的稳定私钥副本。已有人工配置不会覆盖；显式 `false` 保持关闭，省略开关的旧配置升级后默认开启，因此必须保留有效 CA 私钥。SSRF 防护仍默认开启，不随 MITM 默认值变化。此前在透传模式启动的存活沙箱持有临时 CA，切换到稳定 CA 后应刷新其公开 CA bundle，或重启 Manager 重新下载公开 CA，避免出现证书不受信任。

默认 `just dev-init` 在首次缺少输入时准备 `.run/secrets/` 中的 JWT 与 CA 私钥；已有文件不覆盖。需要复用固定私钥时，首次初始化前将原有 `upstream-proxy-ca-key.pem` 和 `code-session-jwt-ed25519.pem` 放到该目录，或通过自定义初始化文件的 `fixed_secrets_dir` 指定来源。已绑定环境缺少输入时要求恢复原文件，不生成替代密钥。

初始化时按 `fixed_secrets_dir` 读取并复制到当前 worktree：

```text
.run/generated/secrets/upstream-proxy-ca-key.pem
.run/generated/secrets/code-session-jwt-ed25519.pem
```

处理规则：

- 复制阶段要求来源为常规文件，不输出文件内容；默认准备入口生成对应格式的私钥。
- 目标不存在时原子复制，目录权限 `0700`，文件权限 `0600`。
- 来源与目标内容相同则跳过；不同则报告冲突，不覆盖。
- 来源缺失时报错，不自动生成替代密钥。
- 来源与目标为同一文件时只校验，不进行自复制。
- 目标文件不纳入 Git，也不提交开发者机器上的私有配置。
- 生成的服务配置使用目标文件的绝对路径。

这两个密钥允许跨环境共用。Brain claim token 和四个 workspace API key 仍按环境独立生成。

## 幂等、更新与凭据保存

| 情况                           | 处理                               |
| ------------------------------ | ---------------------------------- |
| 已登记资源缺失且可确认安全补齐 | 创建并登记                         |
| 已有资源与配置一致             | 跳过                               |
| 已登记资源的可修改配置不同     | 报告差异，使用 `--update` 显式更新 |
| 未登记资源发生名称或身份冲突   | 报错，不自动接管                   |
| 文件移除某项资源               | 保留数据库资源                     |
| API key 已撤销或过期           | 报告状态，不自动恢复               |
| 已登记用户或 workspace 被删除  | 报告冲突，不自动复活               |
| 重复或并发初始化               | 加锁，避免创建副本                 |

`--update` 保留资源 UUID 和 external ID，只更新已登记资源的可修改配置。身份冲突继续报错，不自动迁移组织归属、不删除资源，也不隐式轮换客户端 API key。

四个客户端 API key 和 Brain claim token 使用安全随机值，不从邮箱或环境名称推导。数据库中的客户端 API key 沿用哈希存储。

拟保存以下本地文件：

```text
.run/
  env.json

.run/generated/
  config.yaml
  state.json
  credentials.json
  secrets/
```

- `env.json` 保存名称、环境标识、绑定路径和初始化文件位置。
- `state.json` 保存资源引用、实际 ID、初始化阶段和可恢复状态。
- `credentials.json` 保存客户端 API key 明文、对应 key ID 和 workspace 信息。
- `secrets/` 保存当前环境独立的 Brain claim token 等生成密钥。
- 私有目录权限 `0700`，配置、状态和凭据文件权限 `0600`，全部忽略提交。
- `slot-info` 只显示路径和脱敏信息，不输出密码、token 或私钥。

本地文件与数据库无法共享事务。初始化先原子保存待提交的随机密钥，再写入数据库，提交后标记完成。中断重试复用相同密钥，通过实际资源状态恢复，不重复创建 key。

客户端 key 的明文文件丢失后不能从数据库哈希还原。初始化应报告问题并提供显式轮换入口，不能静默产生新 key。资源映射丢失时也不能仅按名称重建归属。

## 初始化流程与实现边界

```mermaid
flowchart TD
    Input[读取并校验全部初始化文件] --> Lock[锁定环境并检查绑定与归属]
    Lock --> Prepare[校验并复制固定密钥 保存待提交随机密钥]
    Prepare --> Infra[确保独立 Compose 栈与 bucket 可用]
    Infra --> Migration[执行迁移并初始化 secrets 服务]
    Migration --> Identity[创建组织 用户 workspace 和成员关系]
    Identity --> Keys[创建四个客户端 API key]
    Keys --> Environments[按 workspace 初始化无限制网络的 default 环境]
    Environments --> Providers[按 workspace 初始化 model providers]
    Providers --> Complete[保存映射与完成状态 输出地址和凭据路径]
```

使用小型 Go CLI 负责配置解析、初始化和状态查询，Shell 脚本只作为入口及进程管理层。不额外要求安装 `yq`、`psql` 或 `mc`。

组织、用户、workspace、成员关系和 API key 的关联数据库写入保持事务一致。provider 按 workspace 逐个写入，沿用现有 slug 唯一约束。整个初始化跨数据库、文件和对象存储，采用可恢复阶段，不宣称全局事务。

API key 创建必须同时维护 Console 展示记录和实际鉴权记录，复用现有业务语义。不能只插入鉴权表导致页面不可见，或只插入展示表导致 key 无法使用。

所有应用运行时 SQL 使用 Yourbatis。数据库创建和迁移仅在现有启动期维护例外范围内使用原生接口。修改 Mapper 前阅读 `docs/design/be/yourbatis-guidelines.md`，需要 schema 变更时新增 goose migration。

生成的服务配置以当前 worktree 的基底配置为来源。所有现有路径字段先按基底配置目录解析，再写为绝对路径，避免配置移动到 `.run/generated/` 后导致 Vault KEK、manager 等路径失效。原有 Vault 主密钥配置保持可用，不隐式轮换。

初始化失败保留已创建资源和恢复状态，不自动删除数据库或 bucket。重试从实际完成阶段继续；正式启动只接受初始化完成的环境。

## 消息隔离

每个环境使用独立 NATS 集群和持久卷，消息名称保持现有固定值，不新增 namespace 配置或修改业务消息组件。

## 文档与交付范围

实现范围包括：

- Go 初始化 CLI、状态查询和恢复逻辑。
- 环境配置、初始化文件和 provider 文件的命名 schema。
- server、web、DSH 重启脚本及 `just` 入口。
- 基础 seed 与开发初始化的兼容处理。
- 私有文件忽略规则和权限处理。
- 配置参考、DSH README、重启说明及相关后端设计文档。

配置模型变更同步更新严格 YAML 输入模型、运行时配置映射和配置合同测试。仓库中的示例不得包含真实密钥。

## 验收标准

先覆盖失败场景，再覆盖成功场景：

- 配置非法、密钥缺失、资源归属冲突、端口冲突及依赖服务不可用时明确失败。
- 固定私钥内容不一致时不覆盖；来源与目标相同时可重复执行。
- 不同 provider 的同名模型可同时初始化；重复 slug 或修改已创建的 slug 时明确失败。
- 中断和并发初始化不产生重复用户、workspace 或 API key。
- 凭据或资源映射丢失时不静默换 key、不误接管其他资源。
- 新环境创建 `test@oma.ai`、一个组织、两个 workspace，每个 workspace 两个 key。
- 重复初始化不改变资源 ID 和密钥；重启不把测试账号改回原默认账号。
- 四个 key 在 Console 可见，并正确鉴权到各自 workspace。
- 两个 workspace 都能使用文件配置的 providers。
- 两套环境同时运行时，Runner、DSH、后台任务和 NATS 消息不会跨环境处理。
- 两套独立 NATS 集群中的扫描和清理互不影响。
- 两边登录互不覆盖，重启 A 不停止 B 的服务。
- 配置移动后所有相对路径仍正确，前端代理及远端可达地址符合各自配置。
- 未启用开发环境初始化时保持现有行为兼容。

实现后按实际改动执行仓库要求的代码生成、静态检查和单测，包括 Go 的 lint、dead-code、duplicates、complexity，以及前端改动对应的格式、命名、构建和测试检查。

真实双环境集成验收按用户授权范围执行。不得自动运行 `verify-be`；未执行的真实服务、Worker、远端 sandbox 或性能场景需在交付时明确列出。

## 9. 实现说明与初稿差异

实现保留本文全部目标与验收语义，以下细节以代码为准：

- 实现位置：`cmd/dev-init`（CLI 入口）、`internal/devinit`（spec 解析、布局派生、
  注册表、本地状态、固定密钥、编排、配置生成）、`internal/db/dev_init.go` 与
  admin mapper 扩展（组织/用户写入、成员关系、指定明文 key 的双表 API key 插入）。
- 身份是 worktree 的名字：`.run/env.json` 绑定文件随 worktree 走，名字取
  "区分该 checkout 的目录"的 slug——linked worktree 取父目录名（如 codex
  的 `6abc`、手动的 `feature-login`），主 checkout 取仓库 basename
  （`open-managed-agents`）。对外路由与 compose 项目即 `oma-<名字>-api/web`
  与 `oma-<名字>`。名字在绑定时钉死，目录改名后 re-init 通过 env_id 匹配
  刷新注册表路径、名字与栈不变；同 clone 内重名由注册表加后缀（-0000）。
  跨 clone 同名环境在 Docker 资源归属校验时报错，不允许自动复用、重建
  或删除另一 env_id 的资源。整数 slot、
  env_id 短码后缀与 Redis 逻辑 DB 分配都已删除。
- 回收语义：注册表条目只记录名字、env_id、worktree 路径与初始化文件。
  worktree 删除后条目成为孤儿（路径不存在）——孤儿的名字保留不自动复用
  （栈未删，复用会失败）；`just dev-teardown --orphans --yes` 对孤儿的
  compose 项目执行 `down -v`（连数据卷删除）并移除注册表条目，之后新环境
  完整复用名字。本 worktree 环境的显式回收用 `just dev-teardown --yes`
  （不带 --yes 只列出将删除的资源）。
- 端口是运行时状态：初始化与每次重启动态探测空闲 loopback 端口（应用
  40000+/61000+，栈 pg 45432+/redis 46379+/nats 44222+/minio 49000+/
  e2b 43099+ 扫描段），写入 `state.json` 的 `runtime`，重启脚本据此停止
  上一个实例再探测新端口，`dev-init set-runtime --api-port/--web-port`
  重新生成服务配置；没有对外端口算术。
- 基础设施是每环境一个 compose 栈：`deploy/docker-compose/dev-env.yaml`
  定义 postgres/redis/nats（**三节点集群**，与共享拓扑一致）/minio/
  e2b-local，以
  `docker compose -p oma-<name> --env-file .run/generated/stack.env up -d`
  启动。compose 项目名自带 volume/网络前缀隔离，栈内资源用固定名（库
  `claude_api`、bucket `claude-files`、NATS 默认名）；NATS
  三个客户端端口按环境探测发布，监控端口只在 compose 网络内用于
  健康检查；stream replicas 保持默认 3。e2b-local 按环境参数化 `server.addr` 端口与
  `container_name_prefix`，使用 host 网络（要求 Linux Docker Engine 或
  OrbStack，与共享拓扑同一约束）。`set-runtime` 复用健康栈否则 `compose
up -d` 协调重建；teardown 用 `compose down -v` 连数据卷删除；孤儿清理
  按 compose 项目名执行，不依赖原 worktree。未绑定 worktree 继续使用根目录
  共享 compose 拓扑。
- 对外访问使用 portless（`--no-tls`，纯 HTTP）按名字路由：重启入口把
  `oma-<name>-api` / `oma-<name>-web` 注册为静态 alias 指向当前端口，浏览器
  与服务间统一使用 `http://oma-<name>-{api,web}.localhost`（80 端口；未迁移
  到 80 前由 `portless get` 发现实际地址）。未安装 portless 时绑定环境拒绝
  启动；sandbox 面（tunnel.public_base_url）与直接访问仍用裸端口。重启脚本
  自动启动 proxy（80 端口首次需要 sudo）、注册 alias；DSH `OMA_API_URL` 使用名字。Vite 在有 runtime API 端口时直连后端 loopback，缺少该端口时才回退到名字。
- 归属登记：同一 Git clone 的名字与 env_id 登记在 Git common dir 的
  `oma-devinit-registry.json`，使用 flock 互斥。创建后立即保存 `.run/env.json`，
  在固定密钥复制和任何基础设施操作前确定环境身份。同一路径的未完成登记在
  重试时复用；目录移动后以当前真实路径和初始化文件位置刷新绑定与注册表。
- Compose 归属：容器、卷和默认网络都包含 `oma.devinit.env_id` 标签。
  每次复用、协调或回收前检查项目下全部现存资源；标签缺失或 env_id 不符均
  拒绝操作。项目级机器本地锁串行化同名环境的检查与操作，防止不同 clone
  同时通过空资源检查。旧的无身份标签资源不会自动接管或删除。
- 初始化阶段：`pending-secrets → infra → migrated → identity → keys →
providers → complete`，每阶段先落盘 `state.json` 再进入下一阶段；重试按
  实际数据库状态对齐，不产生重复用户、workspace 或 API key。
- 固定密钥：按 `fixed_secrets_dir` 原子复制到 `.run/generated/secrets/`
  （目录 0700、文件 0600）；内容一致跳过、不一致报错、同源同文件仅校验。
- 生成的服务配置：`.run/generated/config.yaml` 从基底 `.run/config.yaml`
  派生，路径字段一律写绝对路径；`server.diagnostics_addr` 若配置则派生为
  后端端口 +1；`code_session` 三个密钥文件指向复制后的固定密钥与
  Brain claim token。重启入口只消费该配置，不做端口推导。
- seed 兼容：生成的配置覆写组织、默认 workspace 和用户信息，并显式设置
  `bootstrap.seed_api_keys: []`。四个客户端 key 都由初始化创建，普通启动
  不再 upsert key，因此不会恢复被撤销的 key，也不会改写 Console 展示状态。
- `--update` 目前支持 workspace 重命名与 provider 的 base_url/backend/
  model_ids/密钥更新；组织重命名和用户 email/name 变更报错而不隐式修改。
- 普通 workspace 成员角色实现为 `workspace_admin`；默认 workspace 沿用
  组织管理员继承规则，不额外写成员行。
- `auth.console_url` 校验放宽为：生产环境必须 HTTPS，`env: dev` 允许
  HTTP，使 `http://sN.localhost:<port>` 可用于本地邀请链接。

## 初始化恢复与审查修复

- 数据库写入前保存组织 UUID、用户 external ID、workspace UUID/external ID
  以及每把 API key 的 external ID。`identity_pending` 和 key 的 `pending`
  标记区分尚未完成的写入与已经登记后被删除的资源，禁止自动恢复已删除记录。
- 组织、用户、两个 workspace 及普通 workspace 成员在同一个 Yourbatis 事务
  中创建。事务提交后中断时，重试校验已预留身份，复用已经创建的整组记录。
- API key 明文与预留 external ID 在写入前落盘。每把 key 的 Console 与鉴权
  记录保持事务一致；某把 key 已提交、阶段状态未保存时，按同一 external ID
  找回记录并补齐其余 key，不能生成新身份后重复插入同一 key_hash。
- 运行中的 API/web 端口在基础设施复用和重复初始化时保持原值，只在第一次
  尚无端口记录时探测。端口更换只由重启入口停止旧实例后执行，保留旧实例
  的定位能力。
- 配置生成先解析基底文件路径，最后写入环境专属 claim token、JWT 和 CA
  路径，避免被基底配置重新覆盖。
- 孤儿回收不依赖已删除 worktree 的 stack.env，也不使用调用方环境的端口
  配置；先校验目标 Docker 项目归属，再删除栈和卷，成功后移除对应路由及登记。

回归测试覆盖失败重试、目录迁移、跨环境资源拒绝、运行端口保留、密钥路径、
以及真实 PostgreSQL 上的身份事务回滚、部分 key 已提交
后的恢复和撤销状态在 seed 后的保持。真实服务的双环境验收仍须单独执行。


初始化单测通过真实 loopback 监听验证端口避让；状态往返核对完整身份映射、运行端口及四把客户端 key，并验证缺失 claim token 文件能从已保存凭据恢复且不轮换密钥。`TestReservedIdentitiesSurviveStateReload` 只覆盖文件重载，数据库部分提交恢复由 `TestAPIKeyRecoveryPostgreSQL` 覆盖。

## worktree 删除前的环境清理

`just dev-recycle --yes [--worktree /absolute/path]` 调用 Go 开发工具的 `recycle` 子命令，默认清理当前 worktree 的开发环境，也允许从同一 clone 的其他 checkout 指定目标。启动包装器先补齐 Go 生成文件，再运行清理程序。worktree 目录及其 Git 登记由外部流程负责删除，本命令不执行 `git worktree remove`。该流程不依赖 Herdr 客户端。

```mermaid
flowchart TD
    A[确认 --yes 与目标属于同一 Git 仓库] --> B[校验绑定、注册表路径和 Docker env_id 标签]
    B --> C[核对服务命令、cwd 和已登记监听端口]
    C --> D[SIGTERM 并等待三个服务退出]
    D --> E[启用全部 profile 执行 Compose down --volumes --remove-orphans]
    E --> R{容器、卷和网络均已移除}
    R -->|是| F[移除 portless alias、环境登记和生成状态]
    R -->|否或检查失败| X[报错并保留登记供重试]
    F --> G[返回成功并保留 worktree]
    G --> H[外部流程决定是否删除 worktree]
```

目标必须是同一 clone 登记的 checkout，允许主 checkout、锁定、有修改或未跟踪文件的 worktree。清理不读取或修改源码的 Git 状态。绑定文件和注册表必须同时指向目标路径且 env_id 一致，Docker 资源继续使用既有所有权标签校验。未绑定目标只有在注册表也没有该路径时才跳过 Compose 清理。

服务识别结合已登记 API/Web 端口与 `ps` 命令，再用 `lsof` 核对 cwd，只接受目标根目录、`web/` 和 `oma-brain-dsh/`。命令匹配同时发现没有监听端口的 Brain、Go 启动器与 Vite。发送信号前再次核对命令与 cwd，避免停止其他 worktree 的进程。等待上限为 30 秒，不自动升级 SIGKILL；停止失败时不清理 Compose。不得并发运行初始化、重启与回收。

服务退出后复用 `dev-teardown` 的资源清理逻辑，启用全部 Compose profile。Compose 清理失败、有残留资源或无法检查残留时保留 alias、绑定、注册表和生成状态，允许重试；确认资源全部移除后删除绑定与 `.run/generated/`，保留 `.run/config.yaml`、`.run/dev-init.yaml`、`.run/secrets/`、源码和共享 LLM 配置。命令退出码为 0 后，外部流程再执行 worktree 删除。重复执行已经清理的目标会跳过 Compose。

普通单测使用隔离的临时 Git clone/worktree 与模拟 Docker，覆盖确认参数、跨仓库拒绝、身份不符、Compose 失败保留状态、停止服务后删除数据卷、清理后保留目录与私钥、重复清理，以及主 checkout、脏文件和锁定 worktree 的保留；进程测试校验命令/cwd 归属。测试不清理开发者的实际环境。

真实 PostgreSQL 初始化恢复测试按仓库集成测试约定运行：`TEST_MIGRATION_DATABASE_URL=... go test -tags=integration ./internal/db ./internal/devinit -run 'TestDevIdentityPostgreSQL|TestAPIKeyRecoveryPostgreSQL' -count=1`。默认 `just test` 不依赖真实数据库。

审查补充的恢复与重启约束：凭证文件丢失时，只要组织或客户端 key 身份已预留，即使 phase 因重跑回退到 infra/migrated，也必须拒绝生成新明文 key。回收前同时验证 registry 和 binding 的 worktree 路径；Stat 权限或 I/O 错误不作为孤儿依据。初始化 YAML 只接受一个文档。Compose up 失败与健康检查失败均进入有上限的重新探测重试。

后端绑定环境在停止旧实例前完成二进制构建；`start-locked` 使用机器本地 flock，从端口探测、配置写入、alias 注册一直保护到 lsof 确认该后端 PID 实际监听。通过继承的 FD 3 通知锁持有者释放锁，随后继续等待前台服务退出。不同 worktree 可以并行编译，但端口分配与开始监听串行执行。Herdr panel 与后台监听进程的归属均以 Git worktree 顶层目录精确匹配，嵌套 linked worktree 不作为当前仓库处理。Brain token 读取失败或为空时在启动前报错。

Provider 回归通过 `TestAPIKeyRecoveryPostgreSQL` 在隔离 PostgreSQL 上验证双 workspace 的组织内唯一 slug、部分创建已提交后的重试、状态重载、旧状态及迁移后 slug 保留、显式更新与模型引用解析，以及同名歧义和已登记记录删除时的拒绝。`TestNextProviderSlug` 覆盖候选分配和长度边界。

## 本地 OpenObserve

`.run/dev-init.yaml` 的 `local_openobserve: true` 为本环境启用独立 OpenObserve，新建本地初始化模板默认 true，已有显式设置保留。首次启用前拉取固定镜像 `public.ecr.aws/zinclabs/openobserve:v1.1.2`。`dev-init` 与三个 restart 入口的 `set-runtime` 会启动同一 Compose 项目的 `observability` profile，检查 `/healthz`，并把 Docker 分配的 loopback 端口写入 `state.json.runtime.openobserve_port`。

该服务使用独立 `openobservedata` volume，限制 2 CPU、1GiB 内存，不发布到非 loopback 地址。`.run/secrets/openobserve.json` 保存自动生成的强密码，重启复用；`.run/generated/openobserve.env` 为 Compose 提供启动凭据。两者权限为 0600。丢失凭据时不要通过生成新密码尝试覆盖已有数据卷，需恢复原文件或显式回收环境。

生成配置开启 `observability.enabled`，后端为 OpenObserve，organization 为 `default`，logs stream 为 `oma_dsh`，使用动态地址。只覆盖当前环境的生成配置，人工 `.run/config.yaml` 不变；本地开发 ingestion/query 使用同一实例的本地管理员凭据，生产部署应分别配置最小权限账号。OpenObserve 凭据不下发 Brain、浏览器或沙箱。

关闭开关停止自动启用，不主动销毁已有数据。`dev-recycle` 与 `dev-teardown` 统一执行 Compose `--profile '*' down --volumes --remove-orphans`，启用全部 profile 后连同 OpenObserve 容器和 volume 一起回收，不依赖当前开关。未启用的 profile 中仍有定义的服务不属于 orphan，不能仅依赖 `--remove-orphans` 清理。

Compose 返回成功后，在同一栈锁内按项目标签重新列出全部容器（含已停止）、卷和网络。仍有资源或无法完成检查时，回收失败并保留 alias、注册表、绑定和生成状态，供修复后重试；只有确认资源全部移除后才删除这些登记。该检查同样适用于孤儿环境回收。私有输入和密钥仍按既有规则保留。

若旧版本漏清理的 profile 资源与新绑定 UUID 冲突，保持归属拒绝，不自动接管或改写 UUID。恢复时应先核对残留资源的旧 UUID、worktree 路径和 Compose 项目，显式清理确认属于旧环境的资源，再使用当前绑定重试回收。

普通单测使用模拟 Docker 覆盖全部 profile 参数、容器/卷/网络单独或同时残留、资源列表失败、Compose 报告项目不存在但仍有资源，以及失败保留状态后重试成功。该测试不删除真实开发环境资源。
