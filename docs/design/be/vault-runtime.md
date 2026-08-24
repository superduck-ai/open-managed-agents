# Vault 存储加密与运行时注入

## 目标

两件事：

1. **存库加密（已完成）**：库被拖走、或只有读库权限的人，都拿不到明文密码。
2. **运行时注入（已完成，Runner 不自动接入）**：显式走 Session MCP HTTP proxy 时可注入凭证；Sandbox / Runner 默认看不到真密码。

主密钥放 `config.yaml` / `kek_file`（全局一把，多 workspace 共用）。本地支持 `version` + `decrypt_only`。不做分片；`KeyProvider` 预留以后接 KMS。

## 背景

Vault CRUD 和 OAuth 注册已经有了；存库侧已切到信封加密。Session MCP HTTP proxy（`/v2/ccr-sessions/{id}/mcp`）仍支持对显式传入的真实 `mcp_url` 注入 `static_bearer` / `mcp_oauth`，但 Managed Agent Runner 不再自动把 MCP config URL 改写到该接口。

## 威胁模型（加密管到哪）

| 场景 | 加密能否挡住 |
|---|---|
| 数据库被偷（备份/磁盘） | 能 |
| 只有查库权限的人偷看 | 能 |
| 数据库 + `config.yaml` 一起丢 | 不能。主密钥就在 config 里，以后分片/KMS 再管 |
| 打进运行中的 OMA 进程 | 不能。本期不解决 |
| 沙箱里靠 prompt injection 骗 Agent 偷密码 | 加密不管。靠后续注入设计：沙箱只见占位符，真密码只在 proxy 里瞬态出现 |

加密只覆盖前两行。沙箱偷密走注入（后续）；进程被拿下是运行时加固，另议；config 一起丢是接受的缺口。

## 加密方案：信封加密

每条密码用一次性 DEK 加密，DEK 再用 KEK 包一层一起存。

信封布局预留了“将来只 rewrap DEK、不必重加密业务密文”的空间（AAD 不绑 KEK version）。**本期本地轮换不做 rewrap**：旧行保留原来的 `wrapped_dek` 与 `key_version`；换主密钥后必须把旧 KEK 留在 `decrypt_only`，直到相关凭证被写路径重新 Seal（换新 DEK / 新 `key_version`）。若在仍有旧信封时从 `decrypt_only` 删掉旧钥，Open 会失败，凭证不可用。

细节：

- AES-256-GCM，每次新 nonce，带 auth tag。
- AAD 绑 `organization_uuid` / `workspace_uuid` / `vault_external_id` / `credential_external_id`（长度前缀字符串）；搬走就解不开。四字段均须非空，`Seal`/`Open` 在空或纯空白时直接拒绝，避免封出之后填 ID 就解不开的信封。故意不绑 KEK 版本，以便将来若做 rewrap 时业务密文可不动。
- 密文头带 key version，老数据自带“用几号钥匙锁的”。
- 解不开就报错。不退化明文，不换别的 key 凑合。

## 主密钥

```yaml
vault:
  master_key:
    kek: <32 字节, base64>   # 必填（或 kek_file）；dev/prod 均无临时密钥
    # 以后可换: provider: shamir | kms | openbao
```

本期从 `config.yaml` 读（与 S3 access key 相同：启动必填，无 ephemeral 兜底），也支持 `_file` 挂载（同 `upstream_proxy_ca_key_file`）。本地可用 `just generate-vault-kek config/secrets/vault-kek` 生成后配置 `kek_file`，或把打印的 base64 写入 `kek`。

“主密钥从哪来”做成可替换模块：`KeyProvider` + 启动时通用 `Prepare`。主流程不写死读 config。以后要防 config 一起丢，加 Shamir 或 KMS provider 即可；业务密文、DB 字段、对外接口不用动。

```go
type KeyProvider interface {
    Prepare(ctx) error
    WrapDEK(...) (wrappedDEK, error)
    UnwrapDEK(...) (dek, error)
}
```

## 数据库

当前相关列：

| 列 | 现状 |
|---|---|
| `auth` | jsonb，非秘密（如 `mcp_server_url`）。`static_bearer` 更新时可改 URL，并同步 `credential_key` |
| `secret_payload` | **已删除**。明文秘密不再落库；仅进程内 transient 用于 seal/open/merge |

`auth` 在 Yourbatis Mapper 中仍以 JSONB 原始字节扫描，但不会作为 `json.RawMessage` 扩散到运行时策略或
API 响应。`internal/vaults` 在数据库边界按 `auth.type` 判别并解析为 `mcp_oauth`、`static_bearer` 或
`environment_variable` 的命名 schema；未知类型或具体字段类型不匹配时 fail-closed。HTTP create/update
请求同样先解码为命名 DTO；create 使用普通 Go 字段，PATCH 使用指针表示可选字段。只有 `auth` 类型判别，
以及 `refresh`、`scope`、metadata 等需要区分省略和显式 `null` 的边界保留原始 JSON。PATCH 在命名 schema
上完成合并和校验，再写回规范化的公开 auth JSON；metadata 因键集合由客户端定义而保留为类型化 map。

新增 migration `00049_add_vault_secret_envelope.sql`（Direct cutover：同一次迁移加信封列并丢弃明文列）：

| 列 | 类型 | 说明 |
|---|---|---|
| `ciphertext` | bytea | AES-GCM 密文（含 tag） |
| `nonce` | bytea(12) | nonce |
| `wrapped_dek` | bytea | 被 KEK 包过的 DEK |
| `format_version` | int | 密文/AAD 格式版本 |
| `key_provider` | text | provider 名（`local` / 以后 `aws_kms` 等） |
| `key_version` | bigint | 用几号 KEK |
| `version` | bigint not null default 0 | CAS 乐观锁 |

**Direct envelope cutover**：同一 migration 增加信封列并 `drop column secret_payload`。无 Expand/Backfill/Contract 双读窗口；既有明文随列一起丢弃，不提供 `backfill_secrets` 维护接口。信封完整性与 active/archived 生命周期由应用写路径强制，**不使用 PostgreSQL CHECK**。

**活动凭证必须有信封**：`CreateVaultCredential` / `UpdateVaultCredential` 在落库前强制完整信封（`ciphertext`/`nonce`/`wrapped_dek` + `format_version`/`key_provider`/`key_version`）；缺信封或残缺字段直接拒绝，防止 Seal 漏调把 NULL 信封写入 active 行。`SealCredentialSecret` 对空/`null` payload 也返回错误（不再 no-op）。既有 active 行若已缺信封：update / validate / open-for-merge 且请求未携带完整替换 secret → HTTP 400，提示客户端重新提交 secret；缺信封但带完整替换 secret（如 `static_bearer.token`）→ 跳过 Open，直接 Seal 写回。信封存在但 Open 失败（篡改 / 错误 KEK / AAD 不匹配）→ HTTP 5xx fail-closed。

**`POST /v1/vaults/{vault_id}/credentials/{credential_id}` 更新合同（缺信封 / CAS）**：

| 请求形态 | 信封状态 | HTTP |
|---|---|---|
| 仅改 `display_name` / `metadata`（无 `auth` 或不含完整替换 secret） | 缺信封 | **400**（与 open-for-merge 相同：要求重新提交 secret；metadata-only 不能绕过） |
| 带完整替换 secret 的 `auth` | 缺信封 | **200**，跳过 Open，直接 Seal 写回 |
| 省略 secret 的部分 `auth` 更新 / preserve-on-omit | 有信封 | **200**，Open → merge → reseal |
| 任意成功路径上的并发写 | `version` CAS 未命中 | **409**（`conflict_error`：Credential was modified concurrently; reload and try again） |
| `credential_key` 唯一冲突（如改 `mcp_server_url`） | — | **409**（Credential key already exists） |

**归档要清秘密**：archive 凭证（以及 archive vault 级联归档凭证）时，除了标 `archived_at`，还要把信封列清空——官方要求 archive“清秘密、留元数据”，不能只软删了事。

## 持久化实现（Yourbatis）

`internal/db` 中 vault / vault_credential 读写已整链迁到 Yourbatis，四文件拆分：

| 文件 | 职责 |
|---|---|
| `vaults.go` | `DB` 对上层暴露的 API、CAS/limit/ErrNotFound 编排、`yourbatis.DB.Transaction` |
| `vault_mapper.go` + `vault_mapper.xml` | `VaultMapper` |
| `vault_credential_mapper.go` + `vault_credential_mapper.xml` | `VaultCredentialMapper`（信封列、`sensitive=true`、归档清密文） |
| `*.sqlmap.gen.go` | `sqlmapgen` 生成，不入库 |

`ArchiveVault` / `DeleteVault` / `CreateVaultCredential` 在同一 Yourbatis 事务内分别构造两个 Mapper，不再使用 `sqlx.Tx`。
运行时凭证加载先通过 `VaultMapper` 批量筛选当前 workspace 中未归档的 Vault，再通过 `VaultCredentialMapper` 按 Vault UUID 批量加载活动凭证；Go 层按原始 `vault_ids` 顺序组装结果，最多执行两次查询。
**Credential secret update（preserve-on-omit）**：更新请求省略 secret 时，Open 现有信封 → merge 非秘密字段 → 用新 DEK reseal，并用 `version` CAS（冲突 → HTTP 409）。缺信封时无法 merge：metadata-only 或未带完整替换 secret → HTTP 400；带完整替换 secret → 直接 reseal。mcp_oauth 合并后完整性只要求 `access_token`，以及配置了 refresh 时的 `refresh_token`；**不**因公开 `token_endpoint_auth.type` 为 `client_secret_*` 而强制信封内有 `client_secret`（平台 OAuth 合法地省略；BYO/DCR 的 secret 在 create / patch `token_endpoint_auth` 时校验）。

KEK 版本由 config 管（`version` current + `decrypt_only` 旧列表）。每条凭证用 `key_version` 标明自己用的是哪把。KEK 本身不进库。

`mcp_oauth_flows` 的 `code_verifier` 与 flow-owned `client_secret`（BYO / DCR）写入 Secret envelope（与凭证同一 `secrets.Service`）；`client_credential_source` 为 `platform | sealed`。平台 client secret **不落用户 flow 行，也不写入 `vault_credentials` 的 sealed refresh payload**；token exchange（以及日后 refresh）按 `mcp_server_url` 从 `vault.platform_oauth_clients` 再解析。Complete/Fail 清空信封列。

Provider/KMS 调用放在 DB 事务外。

> 本期实现：Direct cutover（加信封列 + 删 `secret_payload`）+ CAS + 写路径加密（Vaults API 与 platform MCP OAuth callback）。无 backfill API。

## 轮换

| 层 | 策略 |
|---|---|
| DEK | 每次写/改/刷新密码都换新 DEK。v1 的“轮换”就发生在这层，不用定时任务 |
| KEK | 本地支持 **current + decrypt_only**：新 Seal 只用 current；Open 按信封 `key_version` 在 current∪decrypt_only 选钥。**不做 rewrap**；旧信封保持原 `key_version` 仍可解 |

本地 KEK 轮换操作：

1. 生成新 KEK，把旧 current 挪进 `decrypt_only`（带原 `version`）。
2. 配置新 `kek`/`kek_file` 与递增的 `version`。
3. 滚动重启。新写入打新 `key_version`；旧行继续用 decrypt_only 解开。
4. 从 `decrypt_only` 删除旧钥是运维责任：库中若仍有该 `key_version`，Open 会 5xx。本期不提供批量 rewrap / 退役证明。

```yaml
vault:
  master_key:
    kek: <new-base64-32-bytes>
    version: 2
    decrypt_only:
      - version: 1
        kek: <old-base64-32-bytes>
```

正式 DisableKey / 云 KMS 自动轮换等接 KMS 再说。AAD 故意不绑 KEK version，以便将来若要做 rewrap 时业务密文可不动。

KEK 不做强制退役的原因：config.yaml 模式下旧 key 很难干净销毁；本期价值在「换钥后旧数据仍可读」，不靠扫表迁移。

## 运行时注入（MCP HTTP proxy）

> **状态：已实现但不再由 Runner 自动接入。** 存库加密已完成；MCP HTTP proxy 注入 `static_bearer` 与 `mcp_oauth`（含 refresh / 401 一轮重试）。

目标：显式调用 OMA MCP HTTP proxy 时，由 OMA 在转发前注入凭证；调用方只看到 proxy URL，不看到 token。

注入点：`/v2/ccr-sessions/{code_session_id}/mcp`。显式调用方把真实 MCP URL 放进 `mcp_url`；Proxy 验 JWT、按 Agent Snapshot + Environment 策略授权后，对真实 `mcp_url` 做 Credential URL match，再注入 `Authorization` 并转发。不依赖 upstream CONNECT MITM。Managed Agent 当前保留原始 MCP URL 并通过 `HTTPS_PROXY` 走 CONNECT，因此不会经过此注入点；如需恢复自动 vault 注入，应在 CONNECT MITM 边界单独实现。

### 运行时注入决策（grilling 已确认）

| 项 | 决定 |
|---|---|
| 凭证类型 | **static_bearer** + **mcp_oauth**；`environment_variable` 本切片不做 |
| 注入落点 | Session MCP HTTP proxy（`WithVaultSecrets` → `Injector.WrapTransport`）；**不**走 CONNECT MITM。Runner **不再**自动改写 `mcp_config` 到该 proxy。组装契约：启用 vault wrap 时 `Injector.store` / Secret Service 必须就绪；注入与 refresh 直接使用 `store`，不对 nil store 静默空 plan。`WithPlatformOAuthClients` 接入 `vault.platform_oauth_clients` |
| token endpoint 错误 | 非 2xx 只上报 HTTP status（`token endpoint status N`），不把 IdP `error` 原文带进 error/日志 |
| `expires_at` | 缺失 → 直接注入；存在且 `now >= expires_at` → refresh → reseal → 注入（**无** near-expiry skew）。refresh 写回：`expires_in > 0` 才更新；否则仅当旧 `expires_at` 仍未过期时保留，否则置空 |
| 401 | 上游 401 且为 `mcp_oauth` → refresh 一次再试上游；仍失败 → 跳过该凭证继续 walk。`excluded` / `forceRefresh` 按 **plan 凭证 ExternalID**（`planCredID`）记账，不依赖 refresh CAS 返回行是否带齐字段 |
| 401 重试 body | RoundTrip 前缓冲请求体（上限 32 MiB）；超限 **fail closed**（不静默截断重放），由 `snapshotRequestBody` / `readWithinLimit` 实现。打出的是 clone；`defer closeRequestBody(req)` 保证原始 `req.Body` 在成功/失败路径都关闭（RoundTripper 合同） |
| Open / refresh 失败 | **跳过该条**继续下一条可注入匹配（多 vault / 近似 URL），跳过路径打 Warn（credential_id / auth_type / 脱敏 error）；全部失败 → 502 |
| 运行时错误合同 | fail-closed 出口统一为 `ErrInjectionRejected`（`errors.go` + `injectionRejected`）；客户端文案为 `InjectionUnavailablePublicMessage`。MCP proxy 用 `errors.Is` 映射 502，**不**走 Vaults JSON `ErrorAdapter`。skip 路径内部错误同样由 `errors.go` 命名构造，不在 injector/refresh 内散落 `errors.New` |
| 并发 refresh | 同 credential **短租约**串行换票。组装层注入 `OAuthRefreshLease`：测试/无 Redis 默认进程内 **cap-1 channel 信号量**（ctx 取消不泄漏持有者）；生产 `NewRedisOAuthRefreshLease`（`SET NX`，TTL = `maxOAuthRefreshCASAttempts × token超时 + 5s`，覆盖整段 Hold，nil Redis 拒绝）。持约后重读；`version` CAS 冲突重读并将 force 清为 false，已有未过期 token 则复用（401 仅首轮 force）；exchange 失败后重读，**仅当 `SecretVersion` 前进**时再试并复用。抢约失败则等待租约（尊重 ctx），不并行打 IdP |
| 平台 client_secret | 不进用户信封。callback 与 refresh 共用 `ResolveMCPOAuthTokenClientSecret`。公开 `auth.client_credential_source` 为 `platform`（或遗留 confidential 且信封无 secret）时按 `mcp_server_url` 再查 registry；`sealed` 用信封值。reseal 仍不把平台 secret 写回信封 |
| Credential `networking` | **不做** |
| 数据加载 | **每个 MCP RoundTrip 查库一次**（`vault_ids` + active credentials）；401 walk / 换凭证不重复加载；不缓存明文 token |
| Redirect | **不自动跟随**跨 origin redirect |
| host 未覆盖 | **passthrough** |
| 本切片不做 | `mcp_oauth_validate` 真 refresh；`vault_credential.refresh_failed` webhook |

### 每次 MCP 出站流程

1. Proxy 已认证 session（`authenticateRuntimeSession`）→ 解析 `mcp_url` → 策略授权。
2. 查 code session → workspace → `vault_ids` → 活动凭证（**每个 RoundTrip 一次**）；按真实 `mcp_url` 匹配；按 `vault_ids` 顺序 walk **可注入**凭证（`static_bearer` / `mcp_oauth`）。凭证 auth schema 无法解析 → **fail-closed**；同 scheme、host、effective port 无 path 命中 → **fail-closed**；host 不在任何凭证的 `mcp_server_url` 上 → **passthrough**。
3. Open 信封得到 Transient secret payload。`mcp_oauth` 若已过期则 refresh（CAS reseal）后再取 access token。Open/refresh 失败 → **跳过该条**试下一条。
4. 删掉客户端带给 proxy 的 `Authorization`（session JWT），加 `Authorization: Bearer <token>`，转发真实上游。上游 401 时对当前 `mcp_oauth` 再 refresh 一轮并重试；仍失败则排除该凭证继续 walk。
5. 跨 origin redirect：代理不自动跟；客户端新请求重新匹配。

`mcp_oauth` 匹配、open、refresh、reseal 统一走 `auth_schema` / `secret_schema`（`decodeCredentialAuth`、`decodeMCPOAuthCredentialSecret`），不另维护平行 `json.RawMessage` 结构；`expires_at` 为可选 `*string`（缺失 → 视为未过期直接注入）。写回信封时用命名 struct marshal，token endpoint 响应才是外部 wire DTO。

匹配规则：凭证与请求 URL 的 scheme、hostname、effective port 必须一致；凭证 `mcp_server_url` 的 path 必须是请求 path 的**按 `/` 分段前缀**。例如 `…/mcp` 命中 `…/mcp`、`…/mcp/sse`，不命中 `…/mcp-admin`。`https://host` 不得匹配 `http://host:443`（避免把 Bearer 注到明文 HTTP）。

后续（非本切片）：

- **environment_variable**：环境占位符 / 出口替换（E2B 上另议）
- Credential 级 `networking.allowed_hosts` 门控
- `mcp_oauth_validate` 真探测；`vault_credential.refresh_failed` webhook

显式 MCP proxy 调用方只持有 proxy URL + session JWT，没有上游 token。真 token 只在 OMA proxy 里瞬态出现。日志不记录明文。Runner 生成的 `mcp_config` 则保留原始 URL，不携带 session JWT 或上游 token。

## 实施阶段

0. 本文档：威胁模型、接口、失败用例 ✅
1. Secret Service + 加密 + 本地 provider（KEK 来自 config）+ 契约测试 ✅
2. DB Direct cutover：加信封列 + 删 `secret_payload`；无 Expand/Backfill ✅
3. vault API 收口：写走加密，读只返回元信息；缺信封 → 400；Open 失败 → 5xx ✅（含 platform MCP OAuth callback seal）
4. Transport 注入（static_bearer MVP）✅
5. OAuth 刷新 + mcp_oauth 注入（含 401 一轮）✅；env 占位符；credential networking — **未做**

## 验收（存库加密：已完成）

- DB、日志、trace 不出现明文密码 / DEK；**主密钥只在 `config.yaml` / `kek_file`**（不进 DB/日志）；dev/prod 均须配置，无 ephemeral 兜底。`mcp_oauth_flows` 敏感字段走 Secret envelope；平台 client secret 不落 flow 表，也不复制进用户 `vault_credentials`。
- 写统一经 Secret Service；读不返回密码；`secret_payload` 列不存在。
- 篡改密文 / nonce / wrapped_dek / AAD 后解密失败；未知格式或 key 不可用 → fail closed（HTTP 5xx）。
- 活动凭证缺信封且未带完整替换 secret 的 update/validate（含仅改 metadata/display_name）→ HTTP 400；带完整替换 secret 的 update → 直接 reseal；`version` CAS 冲突 → HTTP 409。
- 归档凭证时清空信封列（不只标 `archived_at`）。
- `POST /v1/vaults/backfill_secrets` 不再注册。
- 本地 KEK：`version` + `decrypt_only`（无 rewrap）。
- `go test ./internal/secrets/ ./internal/vaults/ ./internal/config/ ./internal/db/ ./internal/api/ -count=1`；相关 E2E / `tests/vaults_encryption_test.go`。

## 验收（运行时注入：已完成）

- 显式 MCP proxy 请求走 `/v2/ccr-sessions/{id}/mcp`；vault 注入接在该 proxy（`WrapTransport`），不依赖 MITM。Managed Agent Runner 不再自动生成该 URL。
- `static_bearer` / `mcp_oauth`：path 前缀命中后注入 Bearer；显式 proxy 调用方 / 日志不见 token。Runner 生成的 `mcp_config` 保留原始 URL，不携带 session JWT 或上游 token。
- `mcp_oauth`：无 `expires_at` 直接注入；过期则 refresh + CAS reseal；上游 401 再 refresh 一轮。
- Open / refresh / 401 重试失败 → 跳过该凭证继续 walk；全部失败 → 502。
- host 未覆盖 → passthrough；同 host path 不配 → 拒绝（502）。
- 不自动跟随跨 origin redirect 携带注入头。
- Environment / MCP proxy 网络策略仍生效；本切片不做 credential `networking`。

> 注：云 KMS 自动轮换 / DisableKey 另议。

## Platform OAuth Client（登记）

部署方可在 `vault.platform_oauth_clients` 配置通用 registry（列表），每项绑定精确 `mcp_server_url` + `client_id` + `client_secret`（本地/dev 可内联；勿提交真实 secret）。

`POST .../mcp/vault-auth/start` 解析 client 顺序：

1. 请求带非空 BYO `client_id` → 用 BYO（覆盖 Platform）；`client_credential_source=sealed`，secret 进 flow 信封
2. 否则精确匹配 Platform OAuth Client registry → 只用平台 `client_id`（secret **不**写入 flow / credential）；`client_credential_source=platform`，callback（及日后 refresh）再从配置取 secret
3. 否则走既有 DCR；无 registration endpoint 则失败；DCR secret 进 flow 信封与最终 credential 信封（`sealed`）

`code_verifier` 始终进同一信封。Redirect 仍由前端传入 `{origin}/oauth/vault/success`。控制台 Optional Client 字段保留。用户 access/refresh token 仍进个人 Credential 信封；平台 `client_secret` 不进该信封。

## 不做（本切片外）

- `mcp_oauth_validate` 真 refresh / live MCP probe
- `vault_credential.refresh_failed` webhook 发出
- `environment_variable` 出口替换
- Credential 级 `networking.allowed_hosts`
- Expand/Backfill、`backfill_secrets`
- Shamir / 云 KMS provider 实现
- 重做 vault CRUD、管理页、MCP Catalog/Permission/Confirmation
- 防「打进 OMA 进程」（运行时加固，另议）

## 参考

- https://platform.claude.com/docs/en/managed-agents/vaults
- https://www.anthropic.com/engineering/managed-agents
- HashiCorp Vault：`vault/barrier_aes_gcm.go`、`shamir/`
- Related: #65、#52、#121、#137、#142
- Ubiquitous language: `CONTEXT.md`（Secret envelope / Runtime credential injection / Credential URL match / Platform OAuth Client）
