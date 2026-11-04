# URL 与 API key 配置合同

## 配置来源

连接由 endpoint 和属于该 endpoint 的 workspace-scoped API key 组成。Skill 不内置真实 key，不提供共享测试 key。

- URL：用户本次显式指定 → `OMA_BASE_URL` → 默认 `https://oma.gz.cvte.cn`。
- Key：用户为当前 URL 显式提供 → 当前会话中明确绑定该 URL 的 `OMA_API_KEY` → 提示用户在本地秘密输入。
- 原版 ant 只读取映射后的 `ANTHROPIC_BASE_URL` 与 `ANTHROPIC_API_KEY`。不自动继承已有 `ANTHROPIC_API_KEY`、Bearer token 或官方登录 profile，避免混用平台凭据。

这是 Skill 的解析规则，不是 ant 新增的原生参数。当前不引入项目级配置文件或凭据存储服务。非交互调用缺少 key 时返回缺少配置，不等待输入。

## URL 边界

将 URL 解析为协议、主机、端口；移除尾部 `/`。当前支持部署在 origin 根路径的实例，拒绝 `/v1` 等路径、query、fragment 以及 URL 内嵌用户名密码。不猜测 API 子路径。公网只使用 HTTPS；HTTP 仅用于用户明确指定的 localhost、127.0.0.1 或 ::1 本地开发实例，不关闭 TLS 校验。

在发出带 key 的请求前，确认当前目标与用户授权的 origin 一致。不同 origin（包括不同端口）不共用凭据。地址变化后重新选择该实例的 key，不沿用之前 key；不跟随跨 origin 重定向发送认证，也不使用 curl 的 `--location-trusted`。

## 临时配置

参考 cli.md 在独立 Bash 子 shell 中设置 OMA 变量并映射给 ant，退出该 shell 即释放配置。用户在终端隐藏输入 key，不将真实值粘进可提交的 shell 文件。已有 AWS/Google 路由变量或认证 profile 可能改变 ant 目标；先检查变量名及已选 profile，存在冲突时在子 shell 中清除无关配置，不能输出秘密值或修改用户全局 profile。

建议每个 endpoint/workspace 使用独立工作目录保存 Agent 定义和 claude-lock.json。lockfile 是否包含上游添加的敏感字段需在提交前检查；API key 永远不进入 lockfile、Agent metadata、system 或项目配置。环境变量只是本地临时传递方式，不是秘密保险库。

## Key 生命周期与错误

API key 由用户在目标 OMA 的 API key 管理入口创建，并按当前实现的权限模型选择目标 workspace；不要承诺服务端尚未提供的细粒度 scope。Skill 不自动创建或扩大权限。业务 MCP/Vault 凭据和平台 API key 分开处理。

`ant models list` 是首个只读连接探测。401：检查该实例 key 是否有效或已撤销；403：检查 workspace/权限；TLS 或网络失败：检查 endpoint 与连接。不要输出原始凭据，不对其他域名尝试同一 key，不通过写入操作探测权限。成功查询模型不证明所有写权限或模型运行能力。

轮换时由用户在 OMA 创建替代 key，更新本地临时配置并验证，再撤销旧 key；Skill 不自行撤销用户现有 key。测试记录只保留 endpoint、CLI 版本、资源 ID、状态与脱敏错误。清理本地配置可退出子 shell，或执行：

```sh
unset OMA_API_KEY ANTHROPIC_API_KEY OMA_BASE_URL ANTHROPIC_BASE_URL
```

本次历史验证使用用户临时提供的 workspace key；仓库和 Skill 不保存该值。发布或安装 Skill 不会赋予接收者访问该 workspace 的权限。
