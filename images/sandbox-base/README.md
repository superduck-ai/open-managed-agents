# 运行时固定的 E2B Sandbox 基础镜像

本目录用于构建 issue #75 所需的独立 `linux/amd64` Sandbox 镜像。它不是 Open Managed Agents Server 镜像。其配方精简自 `codex-universal` revision `47f4f0eb5337083e2f610db0d15558932cb4901d` 中固定版本的实践：为每一种受支持的运行时安装一个精选版本，排除 Codex CLI 和多版本管理器，并保持现有 Environment Manager 与 Claude Agent 路径。镜像沿用旧运行合同，以 `root` 作为默认运行账号，`HOME=/root`，Managed Agent 工作目录仍为 `/home/user`；同时预置权限收紧的 `/root/.claude` 和合法空配置 `/root/.claude.json`，保证 Claude Code 首次启动即可安全持久化 root 配置。镜像还保留 UID/GID 1000 的 `user` 和 UID/GID 1001、home 为 `/home/claude` 的 `claude` 兼容账号，两者均使用 Bash 并具有 passwordless sudo。旧 Claude 运行布局中的 `/home/claude/.npm-global`、`/home/claude/.local/bin`、pip cache、Node 全局模块和 Claude 状态目录继续作为共享工具路径；显式切换到 `user` 或 `claude` 时，两个账号也能安全访问这些目录。

## 供应链输入

`versions.env` 是可执行镜像合同中所有平台、运行时、工具版本、下载坐标、校验和和 revision 的唯一值来源。构建 wrapper 从该文件统一生成 BuildKit 参数；Dockerfile 中对应的 `ARG` 不提供重复默认值。相同文件会复制到镜像内的 `/etc/oma-sandbox-versions.env`，供运行时 verifier 读取。固定运行时通过 `/opt/<runtime>/current` 稳定链接暴露，因此 profile、`PATH` 和 verifier 不需要再次硬编码具体版本目录。

该文件同时固定 Ubuntu AMD64 manifest 和干净的 Environment Manager 制品身份。独立拥有的 Environment Manager 不会提交到本仓库。请提供字节内容与固定 SHA-256 匹配的 Linux AMD64 二进制文件：

```console
ENVIRONMENT_MANAGER_BINARY=/absolute/path/environment-manager \
SANDBOX_IMAGE_TAG=oma/managed-agent-sandbox:latest \
just sandbox-image-build
```

允许使用的制品由干净源码 revision `1e719698d8fdb84500bd0c6b356914a4800312e6`（`vcs.modified=false`）构建，SHA-256 为 `f9823cdc138628891427113817a760f299868e1df9aa45b94a775fb113747045`。wrapper 会在 BuildKit 启动前验证仓库固定的 hash，Dockerfile 会再次验证复制到 `/opt/env-runner/environment-manager` 的字节，并通过 `/usr/local/bin/environment-manager` 符号链接保留命令合同。运行时合同还要求版本为 `environment-runner version 1e71969`，并执行其 `task-run` v0 启动路径。CI 通过 `ENVIRONMENT_MANAGER_ARTIFACT_URL` 和可选的 `ENVIRONMENT_MANAGER_ARTIFACT_TOKEN` secret 使用完全一致的制品；由于校验和既不是 secret，也不能由调用方覆盖，因此该 URL 无法替换成不同字节。OMA 不会重新构建或修改 Environment Manager 项目。

所有远程运行时归档只有在与固定校验和匹配后才会被接受。软件包管理器默认使用国内 HTTPS 镜像，不会禁用 TLS 验证，也不会添加不安全的 trusted host。npm、pnpm 和 Yarn Classic 通过共享 `/etc/npmrc` 使用 npmmirror；Bun 通过 `BUN_CONFIG_REGISTRY` 显式使用同一来源。RubyGems 和 Bundler 分别读取共享 `.gemrc` 与 `BUNDLE_USER_CONFIG`，使用腾讯云 RubyGems 镜像；Bundler 不配置官方源 fallback。

## 本地验证

运行来源合同、版本单一真源一致性和 Dockerfile 检查：

```console
just sandbox-image-check
```

完成构建后，验证镜像平台、测量未压缩 Docker 镜像大小，并强制检查默认 `root` 账户、`user`/`claude` 兼容账户、固定运行时版本、真实 Cargo 编译、软件包管理器、实用工具、镜像配置、Environment Manager、可直接调用的 `claude` 命令以及应排除的工具。Yarn、Bun 和 Bundler 的检查会在临时项目中执行最小依赖解析，以确认真实请求只到达配置的国内镜像，因此完整验证需要能够访问这些镜像：

```console
SANDBOX_IMAGE_TAG=oma/managed-agent-sandbox:latest just sandbox-image-test
```

由 containerd 支持的引擎可能会在新加载的层首次挂载并解包前暂时报告零大小，因此测量过程会先运行一个不执行实际操作的 `/bin/true` 容器。随后读取两次 `docker image history --human=false`，并拒绝不稳定结果。它从 `RootFS.Layers` 推导权威的非空文件系统层数（将 OCI 标准空 tar DiffID 计算在内），并要求 history 在汇总精确字节数前，恰好包含这么多非零大小条目。即使截断后的响应所含总条目数仍与 RootFS 层数一样多，这也能拒绝稳定截断。三 GiB（`3,221,225,472` 字节）是报告目标，而不是硬性验收限制：命令会输出 `size_target_status=at_or_below_target` 或 `size_target_status=above_target`，但不会因此拒绝其他方面均有效的镜像。`docker image inspect .Size` 会另外报告为 `storage_size_bytes`，因为由 containerd 支持的引擎可能会在这里暴露压缩后或存储实现特定的值；该值不能替代未压缩体积测量。执行 verifier 前，会先按 SHA-256 比较仓库当前 `versions.env` 与镜像内 `/etc/oma-sandbox-versions.env`，再比较仓库与镜像内 verifier。这样即使旧镜像自己的版本合同和 verifier 彼此一致，也无法针对较新的仓库合同假通过。

2026-07-18 验证的最终本地构建为 `sha256:e428d823477056878f7e675ac7ad399728fd53b930d546925715f6bbbd9f1f12`，包含 `3,401,670,656` 字节的未压缩层数据，比 3 GiB 参考目标高 `180,445,184` 字节。对于同一个镜像，OrbStack 报告的存储大小为 `1,266,108,077` 字节。与只安装 `rustc`、Cargo 和标准库的旧构建相比，保留单版本 rustfmt、Clippy、rust-analyzer 和 LLVM tools 使未压缩镜像增加 `285,638,656` 字节（约 272.4 MiB）；随后加入 `claude` 兼容账号、两个 home 的共享权限、sudoers 及旧 Claude 目录布局合计只再增加 `40,960` 字节（40 KiB），其中恢复 npm/pip/状态目录和 Environment Manager 链接布局只比账号版本增加 `4,096` 字节。切换为 root、补充稳定的 `claude`/`cc`/`c++` 命令和对应合同验证后，未压缩数据相对上一版增加 `28,672` 字节；为 root 初始化 Claude 配置路径又增加 `4,096` 字节。引入镜像内版本合同和固定 runtime 的 `current` 链接后又增加 `32,768` 字节。这些实测成本被接受，不会触发硬性失败。

## 构建与运行时性能

Dockerfile 将从源码构建的运行时和可下载运行时制品拆分到独立 stage。它禁用 Ubuntu 的 `docker-clean` hook，并启用 `APT::Keep-Downloaded-Packages`，使 BuildKit cache mount 可以在 stage 和多次构建之间真正保留已下载的软件包；这些 mount 不会提交进最终镜像。CI 还使用持久化 Buildx layer cache。所有固定制品下载都会在各自 stage 内重试部分传输、HTTP/2 stream 失败和其他瞬时网络错误，避免单个归档下载中断就立即丢弃并行构建结果；每个完成下载的文件仍必须匹配仓库固定的校验和。这一点对经过优化的 Python PGO/LTO 构建尤其重要。最终镜像只复制安装产物，不复制编译器源码树或下载归档。Rust 使用同一份固定 Rust 1.97.0 AMD64 发行包安装 `rustc`、Cargo、标准库、rustfmt、Clippy、rust-analyzer 和 LLVM tools；不引入历史工具链或浮动 channel。

## 发布与部署

`Sandbox base image` workflow 始终会在相关 Pull Request 上检查来源合同和版本单一真源一致性，包括 BuildKit 参数覆盖、无重复 Dockerfile 默认值、动态制品 URL、稳定 runtime 路径，以及 verifier 对镜像内版本合同的读取。手动触发会执行完整 AMD64 构建和直接容器合同验证。显式启用其 `publish` 输入后，它会推送经过验证的同一个 daemon 镜像，确认已发布 descriptor（OCI index）或 config（单 manifest）的身份等于经过测试的本地身份，并记录不可变 registry manifest digest。

发布过程不会部署或替换 E2B Base Template。提升操作被有意设计为独立步骤，必须使用经过验证的 `image@sha256:...` 身份，并获得单独授权。
