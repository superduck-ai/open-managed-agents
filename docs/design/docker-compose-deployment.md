# Docker Compose 一键部署设计

> 目标：让开发者和用户通过一条 `docker compose up -d` 命令启动完整的 Open Managed Agents 本地环境，无需手动安装配置任何中间件。

## 1. 架构总览

```text
docker compose
├── caddy (:28080)         — 前端 SPA + API 反向代理
├── oma-server (:38080)  — Open Managed Agents 主 API 服务
├── e2b-local (:3099)    — 沙箱网关（host 网络，管理 sandbox 容器）
├── postgres (:5432)     — 元数据存储
├── redis (:6379)        — 平台 session 存储
└── minio (:9000/9001)   — S3 兼容对象存储
```

**依赖关系**：

```text
caddy ──→ oma-server ──→ postgres / redis / minio
                    └──→ e2b-local (host.docker.internal:3099)
                              └──→ Docker daemon (宿主机)
                                       └──→ sandbox 容器 (claude-code-interpreter 镜像)
```

## 2. 镜像策略

| 服务 | 镜像来源 | 说明 |
|------|----------|------|
| caddy | `docker.io/library/caddy:alpine` | 官方镜像 |
| postgres | `docker.io/library/postgres:17` | 官方镜像 |
| redis | `docker.io/library/redis:8` | 官方镜像 |
| minio | `docker.io/pgsty/minio:latest` | 社区维护 fork（原版已归档） |
| oma-server | `Dockerfile`（多阶段构建） | Go 后端 + 前端 bun build |
| e2b-local | `Dockerfile`（多阶段构建） | Go 后端 + envd 二进制 |

所有服务镜像均为公开可拉取，无需本地编译。

## 3. 一次性初始化（init containers）

### 3.1 init-web

从 oma-server 镜像提取前端静态产物（`/web-dist`）到 Docker volume，供 Caddy 挂载使用。

```yaml
init-web:
  image: ghcr.io/superduck-ai/open-managed-agents:latest
  entrypoint: ["sh", "-c"]
  command:
    - |
      if [ ! -f /dist-out/index.html ]; then
        cp -r /web-dist/* /dist-out/
      fi
  volumes:
    - webdist:/dist-out
  restart: "no"
```

### 3.2 init-envd

从 e2b-local 镜像提取 `envd` 二进制到宿主机路径。Docker daemon 在创建 sandbox 时需要宿主机可见的路径来 bind-mount envd。

```yaml
init-envd:
  image: ghcr.io/superduck-ai/e2b-local:latest
  entrypoint: ["sh", "-c"]
  command:
    - |
      if [ ! -f /envd-out/envd-linux-amd64 ]; then
        cp /app/envd-bin/* /envd-out/
      fi
  volumes:
    - ./envd-bin:/envd-out
  restart: "no"
```

## 4. 关键设计决策

### 4.1 e2b-local 使用 `network_mode: host`

e2b-local 创建 sandbox 容器后需要访问其动态映射端口（如 `127.0.0.1:32768`）。如果运行在 compose 内部网络中，`127.0.0.1` 指向容器自身而非宿主机，无法访问 sandbox。host 网络模式让 e2b-local 直接使用宿主机网络栈。

### 4.2 oma-server 通过 `host.docker.internal` 访问 e2b-local

oma-server 在 compose 网络中，e2b-local 在 host 网络中。`extra_hosts` 添加 `host.docker.internal:host-gateway` 实现跨网络通信。

**`host.docker.internal` 兼容性**：

`host-gateway` 是 Docker 20.10+ 引入的特性，在不同环境下自动解析为正确的宿主 IP：

| Docker 环境 | `host-gateway` 解析到 | 是否支持 |
|---|---|---|
| OrbStack（macOS） | `0.250.250.254`（OrbStack 宿主 IP） | ✅ |
| Docker Desktop for Mac | `192.168.65.254`（VM 网关） | ⚠️ host 网络模式不支持 |
| Docker Engine for Linux 20.10+ | `172.17.0.1`（docker0 网桥网关） | ✅ |
| Rootless Docker | 取决于 slirp4netns 配置 | ⚠️ 未测试 |

> **注意**：`extra_hosts` 中的 `host.docker.internal` 会覆盖 Docker Desktop/OrbStack 内置的同名 DNS 记录。在 OrbStack 上实测两者解析到相同 IP（`0.250.250.254`），不影响使用。

**e2b-local 必须监听 `0.0.0.0`**：从 compose bridge 网络过来的流量目标地址是 `host-gateway` IP（如 `172.17.0.1`），而非 `127.0.0.1`。因此 `configs/e2b-local.yaml` 中 `server.addr` 需设为 `0.0.0.0:3099`（已完成）。

### 4.3 envd_binary 占位符替换

e2b-local 配置文件 `configs/e2b-local.yaml` 使用 `__ENVD_BIN_DIR__` 占位符。启动时通过 `sed` 替换为宿主机真实路径，确保 Docker daemon 能找到 envd 二进制。

### 4.4 前端内建到 oma-server 镜像

Dockerfile 多阶段构建：Go 编译阶段 + Node/Bun 前端构建阶段。前端产物（`/web-dist`）嵌入镜像，init-web 容器提取到 volume 供 Caddy 使用。

### 4.5 不使用 nginx，使用 Caddy

Caddy 配置更简洁，与项目技术栈一致（都是 Go），Caddyfile 仅 27 行实现 SPA fallback + API 反向代理。

## 5. 认证路由修复

### 问题

`apiEntrypointRouter.ServeHTTP` 基于 Host 头判断路由目标，仅 `localhost:5173`（Vite 开发服务器）和 `oma.duck.ai` 被识别为 platform host。通过 Caddy 反向代理（`:80`）或直连服务器端口（`:38080`）访问时，`/v1/*` 请求被错误路由到 service 认证路径（要求 `x-api-key`），返回 401。

### 修复

改为基于客户端凭证的路由决策：

```go
if auth.ExtractAPIKey(req) != ""       → service 路由
if auth.ExtractPlatformSessionKey(req) != "" → platform 路由
default                                → platform 路由（保留开放路由如 /v1/privacy-consents）
```

PR: https://github.com/superduck-ai/open-managed-agents/pull/6

## 6. 前置条件

1. 拉取 sandbox 模板镜像（由 e2b-local 使用）：
   ```bash
   # sandbox 模板镜像需从 e2b-local 对应的镜像仓库拉取。
   # 具体镜像名和 tag 取决于 e2b-local 的 E2B_TEMPLATE 配置。
   docker pull ghcr.io/superduck-ai/claude-code-interpreter:latest
   ```

2. 配置上游 Anthropic API（在 `.env` 中）：
   ```bash
   ANTHROPIC_UPSTREAM_API_KEY=sk-ant-...
   ```

> **平台要求**：`e2b-local` 使用 `network_mode: host`，仅支持 Linux Docker Engine。Docker Desktop（macOS/Windows）不支持 host 网络模式。

## 7. 启动

```bash
docker compose up -d       # 启动全部服务
docker compose down        # 停止并清理
docker compose down -v     # 同时删除数据卷
```

访问入口：

| 入口 | 地址 |
|------|------|
| 控制台前端 | `http://localhost:${CADDY_HOST_PORT:-28080}`（默认 28080） |
| oma API | `http://localhost:38080` |
| e2b-local | `http://localhost:3099` |
| MinIO Web | `http://localhost:9001` |

## 8. 本地开发模式

如果已经安装了 Go 和 Bun，可以只启动基础设施，后端和前端在本地开发运行。

### 8.1 后端开发

```bash
# 启动基础设施
docker compose up -d postgres redis minio e2b-local

# 本地启动 oma-server
go run .
```

### 8.2 前端开发（HMR 热更新）

```bash
# 启动后端（如果还没启动）
docker compose up -d postgres redis minio e2b-local
go run .

# 另开终端，启动 Vite dev server
cd web
bun install
bun run dev
```

Vite dev server 默认监听 `http://127.0.0.1:5173`，`vite.config.ts` 中已配置 proxy 将 `/api`、`/v1`、`/auth`、`/oauth`、`/web-api` 请求自动转发到 `http://127.0.0.1:38080`（可通过 `VITE_API_PROXY_TARGET` 环境变量覆盖）。

> **注意**：前端开发模式下不需要启动 Caddy 和 oma-server 容器（`docker compose up` 时不要包含它们）。Vite 的 proxy 替代了 Caddy 的反向代理功能，本地 `go run .` 替代了 oma-server 容器。