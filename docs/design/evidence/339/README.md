# #339 验收结果与截图

2026-09-08；独立 PostgreSQL 数据库和 NATS 集群；OMA 使用独立管理员与成员测试账号。

## 验收结果

|验收项|结果|
|---|---|
|Default 名称与成员页|显示大写 Default；成员由组织级管理，提示区域使用 OMA 宽布局|
|默认工作区保护|HTTP 验证重命名、归档、成员写入及同名创建被拒绝|
|Billing 继承权限|资源页显示空态，菜单保留；资源 API 仍返回 403|
|Billing 提权与恢复|同一账号：继承时空态，提权后显示真实资源，恢复后重新空态；API 对应 403 → 200 → 403|
|普通工作区成员|组织管理员自动拥有管理权限；显式授予管理员的成员也显示为管理员，每人一行|
|资源隔离|同一管理员切换普通空间与 Default，七类页面各显示当前空间数据；跨空间资源请求被拒绝|
|成员撤权与 Key|成员撤权后新请求被拒绝；工作区 Key 不依赖创建者成员关系；归档后拒绝新业务|
|HTTP 与 PostgreSQL|73 项真实 HTTP 检查通过；210 项真实 PostgreSQL 专项通过，零跳过|
|前端定向与门禁|Billing 空态 5 项测试通过；成员页 3 项、账号菜单 1 项通过；构建、格式与提交质量门禁通过|

成员增删改由独立管理员调用正式 API，Chrome 使用成员原有登录会话核验结果。

## Default 成员页

![Default 成员页](final/default-members.png)

## Billing 权限变化

|继承 Billing：空态|显式 Workspace Admin：显示资源|恢复 Billing：空态|
|---|---|---|
|![继承 Billing](final/billing-inherited-empty.png)|![提权后](final/billing-elevated-resources.png)|![恢复后](final/billing-restored-empty.png)|

## 普通工作区成员

![组织管理员与显式工作区管理员，每人一行](17-members-oma-wide-layout.png)

## 同一账号切换两个工作区

|资源|普通工作区：存在测试资源|Default：不显示普通空间资源|
|---|---|---|
|Agent|![Agent 普通空间](comparison/agents-ordinary.png)|![Agent Default](comparison/agents-default.png)|
|File|![File 普通空间](comparison/files-ordinary.png)|![File Default](comparison/files-default.png)|
|Skill|![Skill 普通空间](comparison/skills-ordinary.png)|![Skill Default](comparison/skills-default.png)|
|Memory|![Memory 普通空间](comparison/memory-ordinary.png)|![Memory Default](comparison/memory-default.png)|
|Environment|![Environment 普通空间](comparison/environments-ordinary.png)|![Environment Default](comparison/environments-default.png)|
|Vault|![Vault 普通空间](comparison/vaults-ordinary.png)|![Vault Default](comparison/vaults-default.png)|
|Session|![Session 普通空间](comparison/sessions-ordinary.png)|![Session Default](comparison/sessions-default.png)|

## CMA 实际页面对照

|项目|结果|截图|
|---|---|---|
|工作区选择器|Default 与普通空间可见|![CMA 选择器](25-cma-workspace-selector.png)|
|Default 成员|提示在组织级管理|![CMA Default 成员](01-cma-default-members.png)|
|普通空间成员|页面说明 Admin/Billing 继承及 Billing 提权规则|![CMA 继承说明](26-cma-inheritance-notice.png)|
|Default 保护|操作菜单无重命名、归档入口|![CMA Default 保护](27-cma-default-protection.png)|

## HTTP 检查结果

[73 项脱敏结果](http-results.json)

![HTTP 与 PostgreSQL 结果](28-http-postgres-report.png)

## 未通过、未实测与待实现

- 全量 Go 测试中 4 项 NATS workerevents 测试因 `insufficient storage` 失败；不能视为全量通过。
- 完整前端套件存在失败或运行时崩溃；仅声明上述定向测试与构建通过。
- CMA 第二账号的 Billing 提权/恢复、撤权与归档访问未实测，仍属于 #339 验收缺口。
- 未配置真实模型与远程沙箱；Session 创建和读取不代表实际运行通过。
- 本轮未重跑官方及自定义 SDK 全套。
- #336 的成员增删改界面、#338 的失效工作区安全回退尚未完成；普通成员越权直访目前返回 403。
- Billing 按角色区分菜单留待后续实现；当前保留菜单并展示资源空态。
