# GitHub authorization_token 运行时注入沙箱

> Issue: #253
> 日期: 2026-08-16
> 分支: `fix/github-token-runtime-injection`

## 背景

官方 Claude Managed Agents 支持把 GitHub 仓库挂载到 session 沙箱（`github_repository` 资源），其中 `authorization_token` 用于**私有仓库的 clone**（官方示例 `"authorization_token": "ghp_your_github_token"`）。

OMA 当前 **token 存而不用**：update 路由把 token 写入 `SecretPayload` 落库（`internal/sessions/service.go:686-690`），但运行时构造 git source 时**只读 `Payload`（url/mount_path/checkout），从不读 `SecretPayload`**（`internal/environments/managed_agent_runtime_resources.go:28-58`）——私有仓库 clone 必失败。

## 方案

1. **`gitRepositoryRuntimeSource` 增加 `AuthorizationToken` 字段**（`internal/environments/managed_agent_runtime_resources.go`）：`json:"authorization_token,omitempty"`
2. **`resolveManagedAgentRuntimeResources` 的 github_repository 分支**：解析 `resource.SecretPayload` 中的 `authorization_token`，合并进 source（token 为空时不输出该字段）
3. **`gitRepositoryRuntimeSourceJSON` 签名调整**：接收 token 参数（或在 payload 结构中携带）

### 安全边界

- token 只进入 **environment-manager payload 的 sources**（sandbox 启动配置），**不进入** session 公开 API 响应、事件流、日志
- token 由 worker 侧消费（git clone 时用），与 vault 注入同级别的敏感处理

## 测试

- `resolveManagedAgentRuntimeResources`：带 SecretPayload 的 github_repository → source 含 authorization_token；无 SecretPayload → source 不含该字段
- 回归：既有 git source 测试（url/mount_path/checkout 不变）

## 验收

- 私有仓库 session 全流程可用（挂载 → clone → 读文件）
- token 不出现在公开 API 响应、事件流、日志中
- 测试覆盖带/不带 token 的 clone 场景
