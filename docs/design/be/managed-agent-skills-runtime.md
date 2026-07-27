# Managed Agent Skills Runtime

## 背景与目标

Managed Agents API 的 agent snapshot 只保存 `{type, skill_id, version}` 引用。custom skill 和
built-in skill 的每个具体版本都已经是一个规范化的不可变 zip，并由 catalog version row
记录对象存储 bucket、key、大小、SHA-256 和顶层目录。

Claude Code 需要在本地 discovery 目录中看到解包后的目录树。运行时因此把 zip 内容作为
Filestore 的虚拟只读目录直接映射到 `/root/.claude/skills`，不再建立 E2B skill volume，
也不再由 Runner 或 Environment Manager 预热、复制或解压文件。

## 启动流程

Environment Runner 在创建 cloud managed-agent Sandbox 前完成：

1. 从 Session 的 agent snapshot 严格解析 `skills[]`。
2. `type=anthropic` 从 `builtin_skills` / `builtin_skill_versions` 解析版本；`type=custom`
   在当前 workspace 内从 `skills` / `skill_versions` 解析版本。
3. `latest` 在启动时解析为具体 active version row。后续 catalog 的 latest 变化不会改变
   已启动 Session 的视图。
4. 在一只 `sqlx.Tx` 中锁定 Session filesystem 和 namespace，确保 `/skills` 固定根存在，
   并原子替换该 filesystem 中 `kind=archive`、`managed_by=skill_archive` 的 entry 集合。
   替换时旧的活动投影统一写入 `deleted_at`，不做硬删除；新的投影作为新 entry 插入。
5. 创建 Sandbox 后，Runner 直接启动 rclone-filestore 的五个固定 mount；multimount
   在内部对 destination 执行 `MkdirAll`，Runner 不执行独立 mount preparation。
6. `/skills` 使用只读 Filestore Token，直接挂载到 `/root/.claude/skills`。rclone ready
   后才启动 Environment Manager；Environment Manager 不再处理 skill。

每条 archive entry 对应一个具体 skill version zip，保存：

- organization、workspace、filesystem 的稳定 UUID；
- `metadata.skill_source` 和 `managed_resource_uuid` 中的具体 skill version UUID；
- 唯一路径 `/skills/<directory>`；
- archive 的 bucket、key、size 和 SHA-256。

同一 filesystem 内，路径和具体 skill version UUID 都唯一。Snapshot 中两个 skill
若声明相同目录但不是同一具体版本，启动失败，不能让后一个静默覆盖前一个。

技能投影与网络、Packages 和 runtime 提交的组合顺序如下：

1. Runner 先按 Environment 开关解析 Session snapshot 中的 MCP hosts，并在 Provider
   `Resolve` 前覆盖 `mcp_allowed_hosts` Work metadata；空集合也显式写成 `[]`，清除陈旧授权。
2. E2B Provider 将 limited policy 的显式 hosts、Package Manager hosts 和受开关约束的
   MCP hosts 组合成创建时 `NetworkOpts`；畸形配置或 metadata fail closed。
3. `Resolve` 成功后，Runner 解析具体 skill versions，并在 Session filesystem 中原子替换
   archive entries。该步骤只读 catalog metadata，不下载 archive。
4. Sandbox 创建后，Runner 通过固定的
   `environment-manager provision-packages --protocol v1 --stdin` 合同安装 Environment
   Packages；包清单只走 stdin，结果按 protocol version、status 和 exit code 严格校验。
5. Packages 成功后，Runner heartbeat Work、确认 Session 仍为 idle 且未归档，然后启动
   包含 `/skills` 在内的五个 rclone mount 并等待 ready。
6. Runner 标记 Sandbox running，再以单个 sqlx 事务锁定 active Work 与 idle Session，
   读取锁内最终 event 快照，创建 Code Session/initial inbound queue，并发布 Work/Session
   runtime identity。随后才把双凭证通过 stdin 交给 Environment Manager；启动失败会终止
   Code Session、按匹配 identity 撤销 runtime metadata 并清理 Sandbox。

```mermaid
flowchart LR
    A["Session agent snapshot"] --> B["Resolve concrete catalog versions"]
    B --> C["Replace kind=archive entries in one transaction"]
    C --> D["Filestore /skills virtual view"]
    E["Immutable zip objects"] --> D
    D --> F["rclone readonly mount"]
    F --> G["/root/.claude/skills"]
    G --> H["Claude Code discovery"]
```

## 虚拟目录合同

Filestore 对外呈现的是 archive 成员，而不是 zip 文件本身：

```text
/skills/
  pdf/
    SKILL.md
    references/
      forms.md
  xlsx/
    SKILL.md
```

Sandbox 中同一棵树直接位于：

```text
/root/.claude/skills/
  pdf/SKILL.md
  xlsx/SKILL.md
```

`/skills` 是真实的固定一级 directory entry；每个 `/skills/<directory>` 是
`filestore_entries` 中的 archive entry，成员则根据 zip central directory 合成，不逐个
写 entry。archive entry 只借用 catalog 对象，不复制对象，也不计入 `filestore_bytes`。
虚拟文件 UUID 由 filesystem、具体 version UUID 和成员路径确定，Runner 重试不会改变
同一节点的身份。

List、metadata 和 ranged read 都由 Filestore 服务实现。对 `/skills` 本身、其后代，以及
以 `/skills` 为 source 或 destination 的任意 mutation 均返回 `403 permission_denied`。
HTTP 只读 Token 和 rclone `readonly=true` 构成 Sandbox 的只读边界；`/skills` 与其他
只读 mount 统一使用目录权限 `0755` 和文件权限 `0644`。

非递归列举 `/skills` 时，Filestore 直接使用 archive entry 的 `path` 返回一级 skill
目录，不下载 archive。递归列举或访问具体 skill 子树时，才按需加载并校验对应 archive。

## archive 校验与缓存

Filestore 在第一次访问某个具体 archive 时按需下载并建立内存索引：

- 压缩 archive 最大 8 MiB，并同时校验 DB size 和 SHA-256；
- 必须是有效 zip，且只有与投影目录一致的单一顶层目录；
- 顶层必须包含 `SKILL.md`；
- 拒绝绝对路径、反斜杠、NUL、空段、`.`、`..`、重复路径、文件/目录冲突和 symlink；
- 解压大小按 archive header 累加并限制为 500 MiB；
- 读取单个成员时流式解压；range offset 通过丢弃前缀实现，不把解压结果整体缓存。

进程内以 `bucket + key + sha256 + archive path` 为 key，使用最多 20 个 archive 的有界
LRU 缓存压缩 archive 和目录索引。单个压缩 archive 最大 8 MiB，因此缓存中的压缩数据
理论上最多为 160 MiB，另有目录索引开销。同一个 key 的并发 cache miss 通过 singleflight
合并，只有一个共享任务下载、校验并建立索引。共享任务使用独立的 30 秒服务端超时，不继承
首个请求的取消信号，避免 leader 断开导致其他等待者一起失败；每个调用者通过自己的 context
独立等待，取消只会结束该调用者，不会终止或 Forget 仍可服务其他请求的共享任务。所有调用者
都取消后，共享任务仍允许在超时内完成并填充缓存。失败结果不写缓存，后续请求可以重新加载。
archive entry 仍是每次请求的授权事实来源；Session entry 删除后，缓存中残留的字节无法再通过
Filestore 路径访问。

## 生命周期与对象保留

Session filesystem 删除后沿用现有有界 cleanup job。最后一批普通文件退休后，同一事务
软删除该 filesystem 的 directory 和 archive entries。archive entry 只是借用 catalog
archive，不会产生 Filestore 对象清理任务或容量扣减。

Runner 每次全量替换 `/skills` 投影时，会在同一事务中软删除旧的活动 archive entries
并插入新集合。这样被移除或换版的 skill 投影仍可用于审计，活动读取和唯一索引只考虑
`deleted_at is null` 的记录。历史投影不拥有 catalog archive，也不会触发对象回收。

删除 custom skill/version 或用 `seed-builtin-skills --prune` 软删除 built-in catalog row 时，
不立即删除 archive 对象，也不创建通用 `object_cleanup` job。原因是已经启动的 Session
可能仍通过具体 version UUID 投影借用该对象。物理 GC 必须先确认没有任何活动投影引用，
属于独立的 reference-aware catalog GC；当前实现选择保留对象，优先保证运行中 Session
的快照稳定性。

## 移除的旧链路

本方案删除以下运行时机制：

- `skill_prewarm` job、fanout、worker 和启动组合；
- E2B skill volume、manifest hash、`manifest.json` 和 `.ready`；
- Environment Work 中的 `managed_agent_skills_mount` metadata；
- Runner 启动时下载/校验 zip 并写 volume；
- `/mnt/skills`、`/workspace/skills` 解压目录，以及 Claude skill discovery 软链；
- Environment Manager 的 managed-agent skill 解压职责。

迁移 `00032_add_filestore_archive_entries.sql` 直接把 `archive` 加入 entry kind，增加
archive 对象与 ownership 形状约束，为历史活动 filesystem 补齐 `/skills` 根，并清除
遗留的 `skill_prewarm` jobs；整个模型不创建独立的 skill archive 投影表。迁移
`00033_validate_filestore_archive_entries.sql` 单独验证新约束，避免在替换约束的短事务内
扫描历史 rows。两张 catalog version 表仍是 archive 所有权来源，schema 不创建
PostgreSQL 外键。

## 验收重点

- resolver 只读取 DB metadata，不在 Session 启动路径下载 archive；
- `latest` 被钉住为具体 version，archive entry 替换是全量且原子的；
- `/skills` list、recursive list、metadata 和 ranged read 返回 archive 成员；
- checksum、路径穿越、缺少 `SKILL.md` 等损坏 archive fail closed；
- 所有 `/skills` mutation 被拒绝；
- rclone 第五个 mount 直达 `/root/.claude/skills`，destination 由 multimount 内部创建；
- Runner 不写 legacy mount metadata，E2B runtime 不创建 skill volume；
- catalog soft delete/prune 不破坏活动 Session archive entry；
- Session filesystem cleanup 会软删除 archive entry，但不删除借用的 catalog object。
