#!/bin/sh
# Caddy 启动脚本 — 打印访问地址后启动 Caddy
#
# CADDY_HOST_PORT 由 docker-compose 传入（容器无法感知宿主端口映射），
# 用于在启动日志中告知用户实际访问地址。
#
# 如果端口冲突，设置 CADDY_HOST_PORT=0 让 Docker 分配随机端口，
# 然后通过 `docker compose port caddy 80` 查看实际端口。

PORT="${CADDY_HOST_PORT:-28080}"

echo ""
echo "  ┌──────────────────────────────────────────────────────┐"
echo "  │  Open Managed Agents — 控制台                         │"
if [ "$PORT" = "0" ]; then
	echo "  │  端口由 Docker 随机分配，运行以下命令查看：            │"
	echo "  │  docker compose port caddy 80                        │"
else
	echo "  │  访问地址: http://localhost:${PORT}                   │"
fi
echo "  │                                                      │"
echo "  │  如端口冲突，在 .env 中设置 CADDY_HOST_PORT 或设为 0  │"
echo "  │  使用随机端口。                                       │"
echo "  └──────────────────────────────────────────────────────┘"
echo ""

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
