# Yourbatis 使用规范

## 目的与适用范围

本文说明 Open Managed Agents 中 Yourbatis Mapper 的代码组织、SQL 编写、事务、日志和测试规范。
根目录 `AGENTS.md` 中的数据库访问、租户隔离、UUID、schema、日志和测试规则继续适用；本文不放宽
这些边界。`yourbatis-admin-api-keys-prototype.md` 记录 API Key 原型的具体设计，本文是跨资源复用的
通用规范。

新增 Mapper 或迁移既有原生事务时，应先确认整个事务链和受影响资源的迁移边界。不要只迁移一条
SQL，也不要让一次原子操作跨越多个事务实现。

## 文件职责

一个资源的 Yourbatis 实现拆分为四类文件：

| 文件 | 职责 | 不应包含 |
| --- | --- | --- |
| `xxxxs.go` | `DB` 对上层暴露的公共 API、领域参数与结果类型、事务和业务编排 | Mapper interface、生成入口、XML SQL |
| `xxx_mapper.go` | Mapper interface、Mapper 专属查询参数和数据库行类型、`go:generate` 入口 | `DB` 公共业务方法、HTTP 或权限逻辑 |
| `xxx.xml` | SQL、动态 SQL、当前 Mapper 内的公共 fragment 和结果映射 | 业务错误映射、跨 Mapper fragment、运行时逻辑 |
| `xxx.sqlmap.gen.go` | `sqlmapgen` 生成的绑定、构建和扫描代码 | 任何手工修改 |

四类文件放在同一个资源 package 和目录，使用能够直接对应的资源前缀。一个生成入口只负责一个
Mapper interface 和一个 XML 文件。推荐的生成声明为：

```go
//go:generate go tool sqlmapgen -dir $PWD -mapper XxxMapper -sql ./xxx.xml -out ./xxx.sqlmap.gen.go -dialect postgres
```

生成的 `*.gen.go` 不纳入版本控制。Mapper interface 或 XML 变化后运行 `go generate ./...`，不得
通过编辑生成文件修复编译、绑定或扫描问题。Yourbatis runtime 与 `sqlmapgen` 必须由 `go.mod`
固定为同一模块版本。

## Mapper interface

- 第一个参数必须是 `context.Context`，其余参数必须命名，最后一个返回值必须是 `error`。
- 查询类 Mapper 的参数较少时应直接声明为命名参数，通常一到四个参数不为包装而包装成一次性的
  `XxxQueryParams` struct。例如优先使用
  `FindByExternalID(ctx, organizationUUID, externalID string)`，并在 XML 中直接绑定
  `#{organizationUUID}` 和 `#{externalID}`。
- 只有查询条件较多、参数在语义上构成一个需要共同演进的条件对象，或同一组条件会被多个查询方法
  复用时，才为查询定义参数 struct。是否使用 struct 应由查询语义和可维护性决定，不能只为了缩短
  方法签名，也不要让参数 struct 承担输入清理、权限判断或业务校验。
- 同一写入命令包含多个字段时，使用命名参数 struct；不要使用 `map[string]any`、`[]any` 或未命名
  的松散 payload。
- Mapper 方法按数据库意图命名，例如 `FindByExternalID`、`ListPage`、`Insert`、
  `UpdateStatusByUUID`；不要使用无法说明查询范围的 `GetData`、`Execute` 等名称。
- Go Mapper interface 名必须与 XML `namespace` 一致；每个方法必须有一个同名 statement，statement
  `id` 不得复用或与方法错配。
- Mapper 只负责 SQL 生成、参数绑定和结果扫描。输入清理、鉴权、权限判断、游标语义、状态转换、
  分页裁剪和项目错误映射由 `DB` 公共方法或更上层边界负责。
- Mapper 专属的数据库行类型可以带 `db` tag；数据库行、领域模型和 API DTO 语义不一致时分别定义
  并显式转换，不要把数据库 nullable、编码或字段命名泄漏到上层。

SELECT 方法通过 Go 返回签名表达基数：

| 返回签名 | 语义 |
| --- | --- |
| `(T, error)` | 必须恰好一行；零行保留 `sql.ErrNoRows` 语义 |
| `(T, bool, error)` | 允许不存在；零行返回 `found=false` |
| `([]T, error)` | 任意多行；零行返回空 slice |
| scalar 类型与 `error` | XML 显式声明 `result="scalar"` |

只有结果集确实可能很大且能够同步逐行处理时才使用 yield 流式方法。流式消费必须在调用方 goroutine
和当前事务回调中完成，不能把 rows、回调或 executor 传到事务外。

## SQL 与结果映射

- 业务数据一律使用 `#{...}` 绑定，不把值拼接进 SQL。
- `${...}` 只能使用 `yourbatis.Identifier`，或来自应用自身固定文本的可信 Fragment；禁止把请求
  参数、数据库内容或其他不可信字符串传给 `TrustedFragment`。
- token、secret、API key、hash、credential、OAuth code 等敏感参数必须声明
  `sensitive=true`，并通过绑定单测验证敏感标记。
- UUID 字符串直接绑定 PostgreSQL `uuid` 列。除非参数所在表达式无法由 PostgreSQL 推断类型且有
  真实数据库测试证明需要，否则不要增加 `CAST(#{...} AS uuid)`。
- 禁止 `SELECT *` 和 `RETURNING *`。投影列必须固定，动态节点不得改变 SELECT 或 RETURNING 的
  列数量、顺序与语义。
- 计算表达式和重命名列必须显式使用 `AS`；输出列或 alias 不得重复。
- 优先使用带 `db` tag 的静态 struct；字段映射不直观时使用扁平 `resultMap`。不要使用通用 map、
  嵌套对象图或动态结果结构。
- `<include>` 只复用当前 Mapper 内语义一致且共同演进的投影或 SQL fragment，不建立跨 Mapper
  fragment 注册表。

动态 SQL 优先使用 Go 风格表达式 `nil`、`&&`、`||`、`!` 和 `len(...)`，不要依赖未验证的完整
OGNL 语义。条件组合使用 `<where>`、`<set>`、`<choose>` 和 `<trim>`，避免产生空 `WHERE`、
前导 `AND` 或多余逗号。

使用 `<foreach>` 前必须定义空集合的业务语义，并在调用 Mapper 前处理。循环体应无条件生成每个
元素对应的参数，避免过滤后生成 `IN ()`。批量写入不自动分块，调用方需要限制输入数量和总参数量。

## 写入结果与错误

INSERT、UPDATE 和 DELETE 必须显式声明结果模式：

| result | 使用场景 |
| --- | --- |
| `exec` | 调用方只关心成功或失败 |
| `rows` | 调用方需要检查影响行数 |
| `returning` | PostgreSQL 写入需要返回固定投影 |

本项目使用 PostgreSQL，不使用 `result="lastid"`。UUID 或其他业务 ID 应由应用生成、由数据库默认值
生成，或通过显式 `RETURNING` 返回；不要实现隐藏的 `selectKey`、额外查询或反射式字段回写。

具有单行不变量的更新和删除优先使用 `result="rows"`，由 `DB` 公共方法明确检查影响行数是否为
`1`。`0` 行究竟映射为 not found、幂等成功还是冲突，由业务边界决定。需要写入后读取完整实体时，
使用显式 Mapper 查询；如果写入和读取必须原子一致，应放在同一个 Yourbatis 事务中。

使用 `errors.Is` 或 `errors.As` 判断 `sql.ErrNoRows`、`yourbatis.ErrTooManyRows` 和
`*yourbatis.StatementError`，不要依赖错误文本。Mapper 不构造 HTTP 状态或 Anthropic 错误结构。

## 租户范围与事务

organization、workspace 等租户级资源的每条查询和写入都必须在 SQL 中显式绑定对应 UUID；不能
依赖调用者此前已经查询或验证过资源。游标定位、更新条件和关联查询同样必须保持正确的租户范围。

Yourbatis runtime 复用应用通过 `pgx/stdlib` 暴露的同一个 `database/sql.DB` 和底层唯一
`pgxpool`，不得创建或关闭独立连接池。普通方法在调用范围内使用 `d.mapperDB` 构造局部 Mapper，
不要把预绑定 Mapper 作为 `DB` 的长期字段。

事务统一使用 `yourbatis.DB.Transaction` 或 `TransactionOptions`。事务回调中需要的所有 Mapper
都必须使用回调传入的同一个 `yourbatis.Executor` 构造：

```go
err := d.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
    firstMapper := NewFirstMapper(executor)
    secondMapper := NewSecondMapper(executor)
    // 所有数据库操作都通过当前 executor 执行。
    return nil
})
```

同一原子事务链中禁止混用事务 executor、`d.mapperDB`、`d.sql` 或原生 `pgx.Tx`。迁移既有事务时
必须迁移完整事务链，不能只替换其中一条语句。事务 executor 不得保存、异步使用或逃逸到事务
回调之外。

## 日志

进程组装层使用 `yourbatis.SlogLogger` 注入 `component=database` 的 logger，Mapper 和事务沿用
同一个 logger。业务包不创建独立日志 handler，也不重复记录已经由最终处理边界记录的错误。

`YOURBATIS_DEBUG` 只用于本地临时诊断，禁止在生产配置中启用。敏感参数必须依靠
`sensitive=true` 显式标记，不能仅依赖当前 logger 默认不输出参数值的实现细节。运行日志仍不得
记录 token、API key、secret、OAuth payload 或其他凭据。

## 测试与验收

新增或修改 Mapper 时至少覆盖：

1. `go generate ./...` 成功，生成代码可以编译；
2. 每个 Mapper 方法的 statement ID、statement kind、SQL 关键结构和参数顺序；
3. 敏感参数的 `Sensitive` 标记和非敏感参数不被误标；
4. `<if>`、`<choose>`、`<foreach>`、分页方向等所有动态分支；
5. 单行、可选行、多行、scalar、零行、扫描错误、执行错误和影响行数语义；
6. organization/workspace 隔离、稳定排序、游标边界和写入不变量；
7. 涉及 nullable、JSON、数组、RETURNING、数据库 cast、事务或多个 Mapper 协作时的真实 PostgreSQL
   测试。

Mapper 单元测试使用可编程 executor 验证生成 SQL 和绑定，不应只断言最终业务结果。真实 PostgreSQL
测试使用临时表、临时 schema 或回滚事务隔离 fixture，不依赖或修改持久业务数据。完成后继续执行
根 `AGENTS.md` 要求的 `just lint`、`just dead-code`、`just duplicates`、`just complexity` 和相关
测试门禁。
