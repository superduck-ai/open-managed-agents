# =============================================================================
# open-managed-agents — Go 编译 + 前端构建 + 运行镜像
# =============================================================================
#
# 构建：
#   docker buildx build --platform linux/amd64,linux/arm64 --provenance=false \
#     -t ghcr.io/superduck-ai/open-managed-agents:latest --push .
#
# 国内容户可用 --build-arg GOPROXY=https://goproxy.cn,direct 加速。
# 如需使用国内镜像源: --build-arg REGISTRY=docker.1ms.run/library

ARG REGISTRY=docker.io/library

# ---- Go 后端编译 ------------------------------------------------------------
FROM ${REGISTRY}/golang:1.26.2 AS go-builder

ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /oma-server .

# ---- 前端构建 (Bun) ---------------------------------------------------------
FROM ${REGISTRY}/node:22 AS web-builder

WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN npm install -g bun && bun install
COPY web/ .
RUN bun run build

# ---- 运行镜像 ----------------------------------------------------------------
FROM ${REGISTRY}/debian:bookworm-slim

RUN apt-get update -qq \
    && apt-get install -y -qq --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# Go 后端
COPY --from=go-builder /oma-server /usr/local/bin/oma-server

# 前端产物（Caddy 通过 compose volume 挂载使用）
COPY --from=web-builder /web/dist /web-dist

# 运行时资产说明：
# - assets/skills/public: 当前仓库无内置 skills，服务启动时会优雅降级（log "disabled"）。
#   后续若添加 skills，需在 Dockerfile 中增加 COPY assets/skills/public 。
# - environment-manager: 默认路径 /usr/local/bin/environment-manager，本镜像未包含。
#   在 docker-compose 部署中由 e2b-local 提供；如不使用 e2b-local，
#   请通过 YAML 的 environment_runner.manager_path 指向外部二进制。

RUN useradd --no-create-home --shell /bin/false oma \
    && chown -R oma:oma /web-dist
USER oma

EXPOSE 8080

CMD ["/usr/local/bin/oma-server"]
