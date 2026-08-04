# Yourbatis API Keys 原型

## 状态与范围

本实现是一个本地实验原型，用于评估将 `internal/db/admin_api_keys.go` 和
`internal/db/console_api_keys.go` 中 API Key 相关的手写 `sqlx` 查询迁移到强类型 Mapper 接口、
MyBatis 风格 XML 和生成 Go 代码后的可读性与维护成本。
它不改变 Admin API 的鉴权、租户隔离、正常游标语义或数据库 schema。列表接口现在会明确拒绝同时
提供 `after_id` 与 `before_id` 的歧义请求并返回 `400 invalid_request_error`；此前实现会静默优先
采用 `after_id`。

Admin API Key 部分覆盖读取、列表、游标查询和更新。公共 DB 方法继续负责：

- 接收由 HTTP/resource/service 边界完成清理和校验的 organization UUID 字符串，并直接交给
  Mapper 绑定；PostgreSQL 会从 `uuid` 列的比较或写入位置推断参数类型，DB 层不重复调用
  `TrimSpace`、`parseDBUUID`，也不增加 SQL `CAST`；
- 校验 `after_id` 与 `before_id` 互斥，并通过独立的 `FindPageAnchorByExternalID` 查询，在
  organization 范围内将 external ID 解析为由 `created_at` 与 `uuid` 组成的分页锚点；
- 将已解析的锚点交给 `ListPage`：`after_id` 使用 `<` 和倒序查询，`before_id` 使用 `>` 和
  正序查询，确保数据库先取到距离锚点最近的记录；
- 执行 `limit + 1` 分页裁剪并返回 `has_more`；`before_id` 在裁剪后反转结果，使 HTTP 响应始终
  保持 `created_at DESC, uuid DESC` 的统一顺序；
- 将 Mapper 单条读取的 `found=false` 映射为项目统一的 `ErrNotFound`；更新 Mapper 只返回
  `rowsAffected`，公共 DB 方法将 `0` 行更新映射为 `ErrNotFound`，不返回数据库实体。

领域记录、公共参数和 DB 对外方法保留在 `admin_api_keys.go`；生成入口、直接参数形式的 Mapper
接口及分页锚点类型集中在 `admin_api_keys_mapper.go`。SQL、动态过滤条件和结果投影由
`admin_api_keys_mapper.xml` 描述，扫描与绑定代码在本地构建过程中生成。
Mapper 方法按实际用途命名为 `FindByExternalID`、`FindPageAnchorByExternalID`、`ListPage`、
`Insert`、`UpdateByExternalID` 与 `UpdateStatusByUUID`。游标定位和列表过滤拆成两条简单 SQL，避免列表语句中嵌套定位子查询，
也让 cursor 不存在的 `found=false` 语义保持显式。
更新路径使用 `result="rows"` 的 `UPDATE ... FROM ...`，只负责执行更新并判断是否命中记录；
Admin Service 为满足 HTTP 更新接口需要返回完整 API Key 的合同，在更新成功后显式调用
`GetAdminAPIKey`。这样 Mapper 不再包含 `RETURNING`、跨表返回投影或完整实体扫描。

分页锚点只受 organization scope 约束，不重复应用 workspace、创建者和状态过滤。这延续原有的
“organization 内位置锚点”语义：客户端翻页时应保持过滤条件不变；锚点不存在或不属于当前
organization 时返回空页。

Console API Key 部分覆盖 `console_api_keys` 的列表、未归档计数、creator 解析、创建和状态
更新，以及创建与更新时对 workspace `api_keys` 记录的同步写入。`CreateConsoleAPIKey` 和
`UpdateConsoleAPIKeyStatus` 都在 `yourbatis.DB.Transaction` 回调中使用同一个
`yourbatis.Executor` 分别构造 `ConsoleAPIKeyMapper` 与 `AdminAPIKeyMapper`：前者只操作
`console_api_keys`，后者负责 `api_keys` 的插入和状态更新。因此双表写入共享同一事务，不会在
`sqlx.Tx` 与 Yourbatis 之间拆分原子性。更新核心记录时要求恰好命中一行；否则整个事务
回滚，避免两张表的状态分叉。该同步更新直接使用 `api_key_ref_uuid` 定位 workspace API key 记录，
不通过 external ID 进行跨表关联。key prefix、suffix 和 hash 在 Mapper 参数中标记为敏感值。

Console workspace 持久化不在这次 API Key 迁移范围内，仍使用原有 `sqlx` 实现。

## 连接与生命周期

Mapper runtime 使用现有 `sqlx.DB` 包装层持有的同一个 `database/sql.DB`，因此底层仍通过
`pgx/stdlib` 复用应用唯一的 `pgxpool`。应用 `DB` 只持有 `*yourbatis.DB`，不提前保存绑定到
数据库 executor 的具体 Mapper。普通方法在调用范围内用 `NewXxxMapper(mapperDB)` 构造
局部 Mapper 变量；事务方法则在 `yourbatis.DB.Transaction` 回调内使用传入的
`yourbatis.Executor` 构造局部 Mapper，确保所有语句绑定到当前 `*yourbatis.Tx`，不会绕过事务。

Mapper 构造器只包装 executor，不创建连接或持有独立生命周期。Yourbatis runtime 也不单独创建
或关闭连接池；`DB.Close` 继续由现有 `sqlx.DB` 与 `pgxpool` 生命周期负责。进程组装层向
`db.Open` 显式传入 `component=database` 的 `slog.Logger`，`mapperDB` 通过
`yourbatis.SlogLogger` 持有它，事务自动继承相同 logger。成功语句使用 Debug 级别，失败语句
使用 Error 级别；`YOURBATIS_DEBUG` 仍是独立的本地诊断开关，可额外输出带脱敏参数的 SQL 块。

## 已知的产品化门槛

该原型尚不满足仓库当前“所有查询统一使用 sqlx 命名参数及结构体扫描”的质量约束：
Yourbatis 生成 PostgreSQL 位置参数和固定 `rows.Scan`。若要正式采用，需要先作出以下之一的
明确架构决策：

1. 为 Yourbatis 增加符合本仓库约束的 sqlx backend；
2. 评审并记录生成 Mapper 代码的受控例外及其静态安全保证。

Yourbatis runtime 与生成器均由 `go.mod` 固定到已发布的
`github.com/superduck-ai/yourbatis v0.1.1`：应用依赖不再使用本地 module replacement，
`sqlmapgen` 通过 Go `tool` 指令声明，并由 `go:generate` 使用 `go tool sqlmapgen` 调用。
后续升级只需更新 `go.mod` 中的模块版本，运行时和生成器会保持一致。

生成的 `*.gen.go` 不纳入版本控制，由 `.gitignore` 排除。干净 checkout 必须先执行
`go generate ./...`；`just server`、`just test`、Go lint/死代码/复杂度门禁、pre-commit、
GitHub Actions 和 Docker 构建都在消费生成代码前自动执行该命令。XML 和生成器版本因此成为唯一
受版本控制的 Mapper 代码来源，避免生成输出与声明发生漂移。

## 验证

- Mapper 单元测试通过轻量可编程 executor 直接调用 `AdminAPIKeyMapper` 的 6 个方法和
  `ConsoleAPIKeyMapper` 的 5 个方法，逐个验证 statement ID、SQL、参数顺序、optional `found`
  语义、结果扫描、错误传递与影响行数，并确认 organization、workspace 与 creator UUID 输入以
  原始字符串参数绑定；
- 生成 SQL 单测覆盖独立锚点定位、after/before keyset 条件、before 正序取页、动态过滤参数顺序
  和不带 `RETURNING` 的部分字段更新；
- `TestAdminAPIKeyMapperPostgreSQL` 连接项目配置的 PostgreSQL，在回滚事务内创建同名临时表并
  写入四条固定时间的 API Key fixture，直接调用生成的 Mapper 验证 organization scope、
  `FindByExternalID` 的 `found` 语义、稳定的列表顺序、双向分页以及更新影响行数和更新后读取；
  UUID 列的 fixture 写入和查询都直接绑定 Go 字符串、不使用显式 cast；测试不依赖或修改项目
  数据库中的既有业务记录，并验证事务继承应用注入的数据库组件 logger；
- `TestAdminAPI` 在真实 PostgreSQL 上覆盖 missing key、跨 organization 隔离、Get、List、
  双 cursor 拒绝、双向 cursor pagination、更新不存在记录、名称/状态更新、更新后重新查询响应，
  以及更新后鉴权失效；
- `TestConsoleAPIKeyMapperPostgreSQL` 在回滚事务内使用临时表，直接验证 creator 的
  organization 隔离、optional 查询、双表创建、列表、计数、双表归档、nullable 扫描与数据库
  组件 logger，同时覆盖 organization、workspace 与 creator UUID 字符串直接绑定 PostgreSQL
  `uuid` 列的路径；
- `TestTypedUUIDAdminConsoleWorkbenchPostgres` 通过公共 DB 方法验证 Console API Key 的列表、计数、
  双表创建事务和双表归档事务，并在测试结束时清理 fixture；
- Go lint、死代码、重复代码、复杂度和大文件门禁保持通过。
