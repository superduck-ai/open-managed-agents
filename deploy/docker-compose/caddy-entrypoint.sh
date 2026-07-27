#!/bin/sh
# Caddy 启动脚本 — 打印访问地址后启动 Caddy

echo ""
echo "  ┌──────────────────────────────────────────────────────┐"
echo "  │  Open Managed Agents — 控制台                         │"
echo "  │  访问地址: http://localhost:28080                    │"
echo "  └──────────────────────────────────────────────────────┘"
echo ""

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
