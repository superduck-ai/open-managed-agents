# 组织与工作区通用权限

## 两种角色

组织角色只接受 `user` / `admin`，工作区角色只接受 `workspace_user` / `workspace_admin`。两层均表示用户 / 管理员，作用域不同，没有超级管理员或跨组织管理权限。

| 能力 | 用户 | 工作区管理员 | 组织管理员 |
| --- | --- | --- | --- |
| 工作台、开发资源、会话及观测、工作区 API Key | 是 | 是 | 是 |
| 当前普通工作区成员管理 | 否 | 是 | 是 |
| 组织成员、邀请、设置、工作区创建与管理 | 否 | 否 | 是 |
| 组织账单、成本与用量 | 否 | 否 | 是 |

API Key 的原有工作区范围不变，不能取得组织管理或成员管理权限。新注册账号创建自己的组织时成为该组织的管理员；管理员可以在组织内邀请或指定其他管理员。

## 授权与数据

每次用户请求读取最新组织身份，再检查目标空间的组织归属、归档状态和有效成员关系，最后判断动作权限。权限只存在本次 Principal，不缓存到长期 session。普通空间的组织管理员继承管理权限，用户需要有效显式成员。工作区管理员不能取得组织管理或账单权限。

Default 通过 `is_default` 和组织级唯一索引标记；用户按组织身份取得 Default 访问权。禁止改名、归档和成员写入。真实 UUID、外部 ID 和 `default` 别名解析到同一实体。历史 Default 成员不参与授权，新组织、登录和 seed 不写入这些记录。

所有凭据均检查目标空间状态；Workspace Key 不依赖创建者的成员身份。用户越权或归档空间的新业务请求返回 403；授权管理范围内目标不存在返回 404；默认保护和非法角色操作返回 400。旧角色值与 owner 等别名不再作为新请求接受。

Workbench Prompt 路由按 URL 中的工作区及已存储 Prompt 的 `WorkspaceUUID` 重新授权，覆盖详情、修改、删除、Revision 和 KV 子路由；请求头选中的空间不能替代资源所属空间。创建入口沿用固定 Prompt ID 时，同时检查目标空间和已存储 Prompt 的空间。列表不返回其他空间的固定 Prompt。

## 历史角色归并

迁移 `00073_simplify_roles.sql` 在一个事务内归并用户、邀请和工作区成员角色，并收紧三张表的 CHECK 约束。旧组织 `developer`、`claude_code_user`、`billing` 全部变为 `user`；旧工作区开发者、受限开发者和账单角色全部变为 `workspace_user`；管理员保持原值。包含已删除记录与被忽略的 Default 历史成员，身份、资源关联、时间戳和删除状态保持原样。

归并后的用户具有开发资源及工作区 API Key 能力，旧账单角色不再拥有账单权限；需要账单管理的账号必须明确设为组织管理员。迁移 Down 只恢复旧约束，无法还原已经归并的角色，回退前应备份并评估权限变化。客户端与服务端需共同升级；依赖旧角色模型的 #347 邀请 PR 与 #354 账单 PR 必须适配本合同后才能合入，本次不合并或修改这些依赖分支。

## 验证与发布

- `TestOrganizationRoles` / `TestTwoRolePermissions`：角色输入校验和两层权限矩阵。
- `TestSimplifyRolesMigration`：真实 PostgreSQL 旧数据归并、管理员保留、约束收紧与回退行为。
- `TestWorkspaceAuthorizationInheritance`：Default、普通成员授权、成员管理、历史记录、批量查询与 Key 边界。
- `TestWorkspaceMemberMutationObservesCommittedRevocation`：成员变更与组织撤权事务。
- `TestWorkspaceMemoryReadsRejectOtherWorkspace`：真实资源跨空间拒绝。
- `TestWorkbenchPromptWorkspacePermissions`：Prompt 资源跨空间、撤权与归档拒绝，授权成员正常读写。
- `TestWorkbenchAttachmentPermissions`：用户可上传附件和访问开发资源，未授权工作区拒绝上传。
- `TestArchivedWorkspaceAdminRequests`：已归档空间的更新与成员读取均 403。
- `TestWorkspaceScopeChecksAllRoles`：工作区用户和管理员都受跨组织、跨资源、归档和组织撤权边界限制。
- `TestWorkspaceMemberRoleChanges`：用户 / 管理员角色变更和显式关系撤销。

真实 PostgreSQL 集成测试的跳过不计为通过。浏览器与远程模型/沙箱验收单独记录，不能用单元测试替代。

## 主分支迁移衔接

Default 标记迁移使用 00072，角色归并新增 00073，不修改已应用的迁移。曾运行早期分支临时 00060 或 Default 标记 00062 的开发库不能直接套用主分支同编号迁移；测试使用独立数据库。部署前必须核对 goose 历史，禁止仅改版本号或删除历史数据来跳过迁移。
