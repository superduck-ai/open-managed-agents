# Billing 工作区权限

## 范围与依赖

本 PR 基于 #346 的组织身份、Default 标记、工作区状态、成员事务、资源隔离和 Key 边界，仅提供 Billing 专项。#339 / #346 保留通用能力及 Billing 基线兼容；本 PR 上线后采用下述完整规则。成员添加、编辑、移除 UI 由 #336 接入；组织邀请和切换由 #338 提供。

## 继承与成员操作

|场景|有效角色与结果|
|---|---|
|Default|组织 Billing 映射 `workspace_billing`，忽略所有历史成员，不接受显式提权|
|普通空间无显式 Admin|继承 `workspace_billing`，不需要成员记录|
|普通空间显式 Admin|`workspace_admin`，来源 `billing_override`|
|更新为 Admin|写入显式 Admin，重复操作幂等成功|
|更新为 Billing|撤销显式授权并恢复继承，不创建 Billing 成员记录|
|其他角色更新、创建或删除继承访问|400，不能移除继承关系|
|无成员管理权限的 Billing 自行提权|403|
|组织角色降级|未撤销的显式 Admin 保留；没有显式关系则失去普通空间访问|

所有变更在 #346 的同一 Yourbatis 事务内校验操作者、目标用户与空间状态。组织管理员的继承权限、Default 保护、未知角色拒绝、跨组织/归档检查不因 Billing 例外而改变。

## 功能权限与展示

- Billing 可以使用 Workbench，并查看和管理计费信息，符合现有组织角色说明。
- 继承 Billing 不获得开发、API Key、资源轨迹或成员管理能力；普通空间提权后具有工作区管理员能力，仍不能管理组织。
- 工作区成员和列表合并继承关系与显式关系，同一用户只出现一行。Default 角色不受显式记录影响。
- 继承 Billing 的开发资源页面显示空态，菜单保留；按角色区分菜单不在本 PR 实现。空态以目标路由工作区的服务端有效角色为准，不代替后端 403。
- Billing 的继承→提权→恢复使资源请求返回 403→200→403。Workbench 保持可用；归档或组织撤权后的所有入口仍拒绝。

## 数据与发布

不新增 schema，不删除或改写历史 Default 成员，不改变资源归属。恢复继承时撤销普通空间显式授权属于用户授权的业务变更，不是历史数据清理。本 PR 必须在 #346 的通用权限合同之上发布；不能回退到忽略组织、作用域或归档检查的旧版本。

## 验证

- `TestBillingWorkspaceInheritance`：提权/恢复幂等、禁止删除与自提权、组织角色降级、报表、Workbench 和开发权限。
- `TestEffective` / `TestMemberChangeProtection`：Default、普通空间继承与角色规则。
- `TestConsoleWorkspaceBatchAccess`：批量角色查询和继承去重。
- `TestPlatformBillingCannotUseDeveloperEntrypoints`：资源、流式轨迹、代理与上传入口拒绝，Workbench 允许，提权不泄漏组织权限。
- `BillingWorkspaceContent.test.tsx`：目标空间角色、空态、提权后资源与 Workbench 内容。
- 继续运行 #346 的 `TestWorkspaceScopeChecksAllRoles`、归档、Key、迁移和资源隔离测试，确保 Billing 不能绕过共用安全边界。

数据库专项必须使用真实 PostgreSQL，跳过不算通过。历史浏览器截图单独注明日期；CMA 多账号提权/恢复、真实模型和远程沙箱不得以 API 测试或文档对照替代。
