# 组织与工作区成员管理（#336）

本功能基于 main，复用已合并的 #339（PR #346）通用权限及成员事务、#338（PR #347）组织加入与切换上下文。组织角色为 User/Admin，工作区角色为 Workspace User/Workspace Admin，与 main 的角色简化合同一致。

## 产品与权限边界

组织成员是加入组织的人；工作区成员表示这些人在某个空间的有效访问权限。Console API 是网页使用的会话认证接口，不是一种成员身份。对外 Admin API 与 Console API 应共享领域规则。

组织 Admin 管理组织邀请、角色和移除操作。前端只根据当前组织的角色显示入口，其他组织的 Admin 身份不能提供权限。后端重新查询操作者的有效组织成员关系；成员修改事务内再次校验角色，不能仅依赖会话中的历史角色。

组织必须保留至少一名有效 Admin。Console 和 Admin API 的删除、降级共用组织行锁和管理员计数，最后一名管理员变更返回 409。移除组织成员时，在同一事务中软删除组织关系和该组织下的显式工作区关系；其他组织关系、工作区团队资源和 Key 保留。重复移除返回 404。

```mermaid
sequenceDiagram
  participant UI as 成员页面
  participant API as Console 接口
  participant TX as Yourbatis 事务
  UI->>API: 确认移除（会话、CSRF、组织范围）
  API->>API: 校验当前组织 Admin
  API->>TX: 锁定组织并读取成员
  TX->>TX: 检查最后管理员与操作者最新角色
  TX->>TX: 软删除组织与显式工作区关系
  TX-->>API: 提交或回滚
  API-->>UI: 成功或保留错误弹窗
```

普通工作区按拆分后的 #346 基线执行：组织 Admin 继承 Workspace Admin，不能编辑或移除；组织 User 通过显式工作区角色加入，可以修改角色或移除显式成员。Default 的权限全部来自组织角色，历史显式关系不参与计算，不提供独立成员管理操作。

## 工作区 Console 合同

全局会话中间件不对工作区成员路由要求组织管理权限；目录和候选接口在资源层解析 URL 中的目标工作区并校验成员管理权限，写入接口在事务内复查目标工作区的成员管理权限。组织普通成员获授 Workspace Admin 后可以管理该空间成员，但不能管理组织或未授权的其他空间，且不能依赖会话当前工作区的角色替代目标空间校验。

路径前缀为 `/api/console/organizations/{organizationUuid}/workspaces/{workspaceId}`。

| 方法与路径 | 合同 |
| --- | --- |
| GET `/members` | 返回 `workspace_id`、`is_default`、`can_manage_members`、`members` |
| GET `/member-candidates` | 返回同组织内尚可添加的成员，字段为 `user_id`、`name`、`email` |
| POST `/members` | 以 `user_id`、`workspace_role` 添加显式成员 |
| POST `/members/{userId}` | 更新显式 `workspace_role` |
| DELETE `/members/{userId}` | 移除显式成员 |

每个成员返回身份、姓名、邮箱、组织角色、有效工作区角色、角色来源及 `can_edit`、`can_remove`。界面消费服务端权限，不自行复制继承算法。目录与候选查询复用 `ListWorkspaceMemberFacts`；添加、改角色和移除统一走 `workspaceaccess.ChangeMember`（组织成员行锁 → 工作区锁 → 操作者授权 → Default 保护 → 显式角色规则）。组织 Admin 的继承访问不可编辑或移除；User 显式成员可编辑和移除；候选列表排除 Admin 与已有显式成员。添加与更新仅提供 Workspace User、Workspace Admin。

## 组织成员 Console 合同

组织成员写路径（改角色、移除）挂 `requireConsoleOrganizationAdmin` 中间件，任何登录成员包括其他组织管理员都不可操作。DB 层 `withOrganizationMemberChange` 在组织行锁事务内校验操作者最新角色与最后管理员计数；Admin API 的 `UpdateAdminUserRole`/`DeleteAdminUser` 与 Console 共用同一保护。移除最后管理员返回 409 `last_organization_admin`，重复移除返回 404，非管理员返回 403。

## 页面与 CMA 对照

组织成员页提供计数、权限说明、搜索、角色筛选、行内角色选择和更多操作菜单；普通工作区成员页额外支持姓名/邮箱/角色排序。无权限时保留 main 的明确权限提示，不请求成员目录；服务器返回 403 或当前权限撤销时隐藏缓存目录。组织右上入口为 Invite，工作区为 Add to Workspace。移除使用确认弹窗，提交期间禁用操作，后端失败保留弹窗和原因。切换组织或工作区后重置弹窗状态。

已观察 `platform.claude.com` 组织成员和普通工作区成员页的布局、权限说明与操作入口。真实 CMA 的移除操作未执行；添加弹窗因无可添加成员未能观察。本地验收需要逐项对照布局、筛选、表格、角色变更、添加和移除流程，不能将页面观察视为写操作验收。

## 验证状态

- 组织角色授权、跨组织拒绝、最后管理员删除/降级、并发管理员变更、级联移除和重复移除由独立 PostgreSQL 事务测试覆盖（`TestOrganizationMemberChangesPostgreSQL`）。
- Console 组织管理员中间件与错误合同由 `internal/platformapi/console_member_authorization_test.go` 覆盖。
- 组织移除取消、CSRF、后端 409 保持弹窗由 `OrganizationMemberRemoval.test.tsx` 覆盖；工作区只读、Default 提示、继承角色、搜索和空候选状态由工作区成员页测试覆盖。
- `TestConsoleMembersAPI` 覆盖组织成员列表、改角色、移除；工作区成员 Console 合同由`TestConsoleWorkspaceMemberTargetAuthorization` 的真实 HTTP/PostgreSQL 测试覆盖，包含跨空间拒绝和管理员降级后目录、候选与写入拒绝。
- 历史全量测试结果及截图按对应提交保留在 PR 验收评论；本轮同步后的验证结果单独记录，不沿用历史结果声明当前提交全绿。
- 本阶段不处理长连接撤权、运行中任务迁移或凭据归属模型变更。

成员资料与权限事实由一次带组织和工作区范围的查询返回，不再拼接截断为 1000 人的组织资料列表；候选成员复用同一查询。工作区页面只保留路由实际使用的 `settings/WorkspaceMembersPage.tsx`。

审查回归覆盖：User 显式成员的编辑/移除能力、Admin API 最后管理员冲突映射、1002 人目录的真实 PostgreSQL 资料扫描与跨组织拒绝。

添加成员弹窗沿用原 PR 的结构：标题包含当前工作区名称，成员和角色选择器占满表单宽度，默认角色为 Workspace User；移除独立搜索框、说明段落及取消按钮，保留右上角关闭和底部添加到工作区按钮。候选加载、错误及空态保护保持不变。

## 2026-10-11 主干适配

#346、#347 已合并，#350 直接基于 main 提供成员管理的完整前后端实现，不重复引入旧依赖提交或迁移。保留 main 的两角色合同、邀请投递提示、无权限视图、组织与工作区切换隔离。账号菜单和组织设置页沿用 main 已更新的实现。

本轮验证结果记录于 PR 描述；历史截图和测试结果仍按原提交保留，不能作为本轮验收证据。
