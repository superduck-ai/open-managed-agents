# 配置 OMA URL 与 API key

## 选择目标

URL 使用用户本次明确指定值，其次使用 `OMA_BASE_URL`；没有配置时询问，不预设任何固定域名。地址可随部署变化。URL 为 API origin，例如 `https://你的OMA域名`，不附加 `/v1`；移除尾部 `/`，拒绝 URL 内嵌用户名密码、query、fragment 和非根路径。

公网使用 HTTPS；用户明确选择本地开发时支持 localhost、127.0.0.1、::1 的 HTTP。不要关闭 TLS 校验或跨 origin 转发 key。

API key 使用用户为该 URL 提供的 key，或当前会话中明确绑定该 URL 的 `OMA_API_KEY`。切换域名、端口或 workspace 时重新核对 key，不自动沿用官方 Anthropic 或上一个实例的凭据。不要将 key 写入 Skill、Agent 文件、lockfile、日志或 Git。

## 临时配置

在独立 Bash 会话中输入真实 URL 和 key，以下地址是占位符，必须替换：

```bash
export OMA_BASE_URL='https://你的OMA域名'
printf '请输入该 OMA 实例的 API key: '
IFS= read -r -s OMA_API_KEY
printf '\n'
export OMA_API_KEY
export ANTHROPIC_BASE_URL="${OMA_BASE_URL%/}"
export ANTHROPIC_API_KEY="$OMA_API_KEY"
ant models list --format jsonl
```

原版 ant 读取 ANTHROPIC 变量，OMA 变量用于明确配置来源，不是新增 CLI 参数。检查已有云平台路由变量和 profile，避免它们改变请求目标；不要打印秘密或修改用户全局配置。非交互环境缺少配置时返回明确错误，不等待输入。

模型列表查询只验证基础连接和认证，不证明写权限或模型可执行。401 检查该实例 key，403 检查权限和 workspace；连接失败检查 URL、TLS 和网络，不向其他域名试发相同 key。

每个实例/workspace 使用独立目录保存资源定义和 claude-lock.json。更换部署前备份相应配置，不将旧资源 ID 当成新实例中的资源。

## 轮换与退出

key 由用户在目标 OMA 管理入口创建。需要轮换时配置新 key、验证，再由用户撤销旧 key；不自动扩大权限或撤销现有 key。第三方 MCP/Vault 凭据与平台 key 分开管理。

退出临时 shell，或执行：

```bash
unset OMA_API_KEY ANTHROPIC_API_KEY OMA_BASE_URL ANTHROPIC_BASE_URL
```
