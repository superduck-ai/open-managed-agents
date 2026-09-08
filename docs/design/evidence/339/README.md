# #339 Chrome 与独立账号验收证据

2026-09-08。在本工作区独立 PostgreSQL 数据库和 NATS 集群运行，未停止其他 worktree 服务。OMA 登录使用专用测试邮箱；成员操作使用正式管理 API，浏览器保持原登录会话检查变化。截图编号 04 是被同域其他登录干扰的准备过程，未纳入验收证据。

## 本轮修复

- 工作区和组织成员页使用 OMA 宽布局，表格保持现有组件和间距；账号菜单显示有效工作区角色。
- Memory Store、Memory 内容、目录及版本读取不再从工作区回退到组织范围，补真实 PostgreSQL 回归。
- Agent 详情切换工作区和 Default 保留名称错误文案已复验。

## Chrome 证据索引

|场景|截图|结论|
|---|---|---|
|CMA Default 成员提示|[01](01-cma-default-members.png)|组织级管理提示，未包含真实账号|
|OMA Default 提示|[24](24-default-members-wide-layout.png)|采用 OMA 宽布局，并可跳组织设置|
|Default 同名创建|[03](03-default-reserved-name.png)|明确提示保留名称，不能创建|
|普通空间创建|[05](05-admin-created-workspace.png)|创建后选择器切换到真实工作区|
|Agent 详情切换工作区|[06](06-agent-before-switch.png)、[07](07-agent-after-switch.png)|回到目标列表，无旧 Agent not found|
|普通成员可见性|[08](08-user-workspace-visibility.png)|只有 Default|
|成员加入与撤权|[09](09-user-before-membership.png)、[10](10-user-after-membership.png)、[11](11-user-after-revocation.png)|同一登录会话：拒绝、可访问、拒绝|
|Billing 继承可见性|[12](12-billing-inherited-visibility.png)|可见普通工作区|
|Billing 提权与恢复|[13](13-billing-before-elevation.png)、[14](14-billing-after-elevation.png)、[16](16-billing-restored-inheritance.png)|同一登录会话：开发拒绝、可访问、恢复后拒绝|
|组织管理员自动拥有工作区管理权限|[15](15-billing-elevated-members.png)|组织管理员无需添加；Billing 提权后也显示为工作区管理员，每人一行|
|成员页 OMA 风格修复|[17](17-members-oma-wide-layout.png)|内容区 1608px、表格 1544px；有效角色文案|
|七类资源创建后的页面（原始冒烟证据）|[18 文件](18-files.png)、[19 技能](19-skills.png)、[20 记忆](20-memory-stores.png)、[21 环境](21-environments.png)、[22 Vault](22-vaults.png)、[23 会话](23-sessions.png)、[06 Agent](06-agent-before-switch.png)|真实创建后的非空列表/详情可见|
|CMA 工作区选择器|[25](25-cma-workspace-selector.png)|Default 和普通空间可见|
|CMA 普通成员继承规则|[26](26-cma-inheritance-notice.png)|实际页面说明 Admin/Billing 继承及 Billing 提权|
|CMA Default 操作菜单|[27](27-cma-default-protection.png)|只有 Manage API keys，无改名或归档|

早期截图中的固定“管理员”文案是本轮发现并修复的问题；最终布局和文案以 17、24 为准。

## 无 UI 的合同检查

[脱敏 HTTP 结果](http-results.json)共 73 项通过：48 项角色与管理权限、15 项资源作用域、10 项 Key 与归档。没有记录 Cookie、验证码会话或 Key 内容。

成员管理页面目前仅提供列表，加入、撤权、Billing 提权/恢复由独立管理员通过正式 API 执行，随后在成员的 Chrome 会话中观察效果。没有将 API 操作冒充为界面操作。

PostgreSQL 专项回归 210 项通过、零跳过，包含迁移、历史默认成员保留、并发撤权、Mapper 绑定、角色继承与 Memory 跨工作区拒绝。前端成员页 3 项测试、账号菜单 1 项定向测试、构建和质量门禁通过。

## 未通过或未实测

- 本轮 `just test` 的 4 项 NATS workerevents 测试因 `insufficient storage` 失败；权限、资源、数据库包及 tests 包通过。未清理其他数据、降低流容量或跳过测试来规避。
- 完整前端套件此前存在失败/运行时崩溃，不声明全套通过。
- CMA 没有可登录的第二成员账号，未实际执行 Billing 提权恢复、撤权和归档后的成员访问。
- OMA 未配置真实模型和远程沙箱，Session 创建/读取通过不代表模型调用、远程执行或长连接运行通过。
- 本轮没有重跑官方及自定义 SDK 全套；上一轮记录保留在主验收文档。


## 截图说明修正与相邻任务边界

- Default 作为显示名称统一首字母大写；路由别名仍是 `default`，不修改历史数据库名称或成员行。
- 09、11 是故意直访无权访问的普通工作区，不能代表正常浏览 Default。现有选择器回退到了 Default，但路由仍指向被拒绝的工作区，是待完成的上下文一致性体验。#336 的成员移除闭环要求接入 #338 的安全回退；当前 PR 尚未实现这一完整回退，不能写作已完成。
- Default 属于组织。尚未加入普通工作区的成员可以访问所在组织的 Default，但具体功能仍由组织角色决定：User 不获得 Agent 开发权限，Developer/Admin 才能读取相应资源。跨组织切换及个人组织身份恢复由 #338 负责。
- 成员页所展示的是：组织管理员无需手动加入就有管理权限；Billing 被授予 Workspace Admin 后也显示为管理员。每个人只显示一行。成员添加、修改、删除的 Console 操作界面属于 #336，本 PR 提供授权规则和接口。
- 原 18–23 截图仅说明测试资源确实创建并能显示，不证明切换工作区后隔离正确。新增同一管理员、相同两个工作区的成对截图见下表。
- CMA 的第二账号提权/恢复、撤权和归档访问未实测，属于本次 #339 验收缺口，不能因为 #336/#338 有相关功能就算覆盖。CMA 本轮已做的只读页面对照保留；OMA Billing 后端提权/恢复及新请求即时生效已测，UI 操作待 #336。

|资源|普通工作区：存在测试资源|切换 Default：不显示普通空间资源|
|---|---|---|
|Agent|[普通空间](comparison/agents-ordinary.png)|[Default](comparison/agents-default.png)|
|File|[普通空间](comparison/files-ordinary.png)|[Default](comparison/files-default.png)|
|Skill|[普通空间](comparison/skills-ordinary.png)|[Default](comparison/skills-default.png)|
|Memory|[普通空间](comparison/memory-ordinary.png)|[Default](comparison/memory-default.png)|
|Environment|[普通空间](comparison/environments-ordinary.png)|[Default](comparison/environments-default.png)|
|Vault|[普通空间](comparison/vaults-ordinary.png)|[Default](comparison/vaults-default.png)|
|Session|[普通空间](comparison/sessions-ordinary.png)|[Default](comparison/sessions-default.png)|
