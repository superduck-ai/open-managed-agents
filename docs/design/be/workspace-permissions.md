# 组织与工作区通用权限

## 范围

#339 / #346 保留组织成员校验、工作区归属与状态、Default 身份和保护、资源隔离、即时撤权、归档保护、Workspace Key 边界及共用权限设施。本分支在 #346 通用能力之上实现 [Billing 完整合同](./billing-workspace-permissions.md)，由基于 #346 的独立 PR 承接，包括自动继承、提权与恢复、角色变更、功能限制、前端空态和专项验收。成员管理 UI 属于 #336，邀请与组织切换属于 #338。

## 授权与数据

每次用户请求读取最新组织身份，再检查目标空间的组织归属、归档状态和有效成员关系，最后判断动作权限。权限只存在本次 Principal，不缓存到长期 session。普通空间的 Org Admin 继承管理权限；Billing 采用 [专项继承规则](./billing-workspace-permissions.md)，其他角色需要有效显式成员。工作区管理员不能取得组织管理权限。

Default 通过 `is_default` 和组织级唯一索引标记；用户按组织身份取得 Default 访问权。禁止改名、归档和成员写入。真实 UUID、外部 ID 和 `default` 别名解析到同一实体。所有历史 Default 成员原样保留且不参与授权，新组织、登录和 seed 不写入这些记录。迁移只回填默认标记，无法唯一识别则原子失败。

所有凭据均检查目标空间状态；Workspace Key 不依赖创建者的成员身份，不取得组织权限。用户越权或归档空间的新业务请求返回 403；授权管理范围内目标不存在返回 404；默认保护和非法角色操作返回 400。

## #346 的 Billing 基线边界

对照 #346 与 main 的共同基线 `4e407449`：普通空间仍要求显式成员；没有成员时更新返回 404；更新 `workspace_billing` 写入成员记录，删除可撤销该显式关系。创建仍不接受 `workspace_billing`。#346 不引入 Billing 自动继承、提权幂等或恢复继承规则；本分支改用 Billing 完整合同。

#346 的 Billing 资源与 Workbench 入口保留基线行为；本分支仅保留 Workbench，开发资源按 Billing 合同限制。这不跳过最新组织身份、跨组织/跨资源归属、成员撤销和归档检查，也不授予组织管理权限。#346 的 Default 统一按组织身份可用并忽略历史记录，其中 `workspace_billing` 仅保留角色标识；本分支额外实现普通空间 Billing 继承。

## 验证与发布

- `TestWorkspaceAuthorizationInheritance`：Default、普通成员授权、成员管理、历史记录、批量查询与 Key 边界。
- `TestWorkspaceMemberMutationObservesCommittedRevocation`：成员变更与组织撤权事务。
- `TestWorkspaceMemoryReadsRejectOtherWorkspace`：真实 Memory 资源跨空间拒绝。
- `TestArchivedWorkspaceAdminRequests`：已归档空间的更新与成员读取均 403。
- `TestWorkspaceScopeChecksAllRoles`：Developer 与既有 Billing 账号均不能越过跨组织、跨资源、归档和组织撤权边界。
- `TestBillingWorkspaceInheritance`：本分支以继承、提权恢复、角色变更和功能权限专项替换 #346 的 Billing 基线兼容测试。

上述集成测试必须运行真实 PostgreSQL，跳过不计为通过。发布后只允许回退到保留通用授权合同的兼容版本；历史成员记录仍存在不代表旧授权逻辑安全。浏览器截图、CMA 多账号与远程模型/沙箱的实测状态分别记录在 PR 验收评论，不能用单元测试替代。
