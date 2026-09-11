# Memory Store Attach 表单

## 目标

Create Session 与 Create Deployment 可以选 Memory Store、Access 和 Instructions。提交体只带客户端允许的字段；服务端 `mount_path` / `name` / `description` 可以只读展示，不能回写。

后端 attach 合同见 [memory-store.md](../be/memory-store.md) §7.1。本文只描述控制台表单。

## 提交体

每个已选 store 写入：

```json
{
  "type": "memory_store",
  "memory_store_id": "memstore_abc",
  "access": "read_write",
  "instructions": "有新偏好就更新"
}
```

- `access` 为 `read_write` 或 `read_only`。UI 选了 store 就显式发送，默认 `read_write`。
- `instructions` 按 Unicode 码点计数，上限 500；空字符串时省略该字段。
- 不发送 `mount_path`、`name`、`description`。编辑已有 Deployment 时，即使 GET 响应带有这些字段，POST 也只回写上面四个允许字段。
- Session 的 `resources` 为文件资源后接 memory 资源。未选 store 时不出现 memory 项。

## 表单

Create Session 的 Resources 与官方控制台一致：`+ Resource` 菜单里选 Memory store，生成一张卡片（选择 store、Access 下拉、可选 Instructions）。Access 为 `read_write` / `read_only`，文案为 Read & write / Read only。Instructions 标签旁标 (optional)，输入框占位为 “Tell the agent what this store contains and when to use it.”。Instructions 按 Unicode 码点计数，上限 500；超过时禁用提交，不截断。服务端 `mount_path` 仅在编辑已有挂载时只读展示。Store 选择器通过 `GET /v1/memory_stores?beta=true&limit=100&include_archived=false` 加载当前 Workspace 的活动 store；响应存在 `next_page` 时继续请求，直至完整列表。选择器只在已加载选项上按名称或 ID 过滤，不另做服务端搜索。

## Store 详情

平台沙箱文件 `/MEMORY.md` 不是 store 内容。列表、树和 Add memory 都不展示该路径；Add memory 也不能提交 `/MEMORY.md`。

## 实现与验收

- `web/src/features/managed-agents/resources/memory-attach.ts`：组包、500 码点、禁字段剥离
- `web/src/features/managed-agents/resources/MemoryStoresAttachField.tsx`：Memory store Resource 卡片
- `web/src/features/managed-agents/sessions/SessionFileResourcesField.tsx`：`+ Resource` 菜单（File / Memory store）
- `web/src/features/managed-agents/api.ts`：Session 与 Deployment 创建、更新请求体；Store 选择器分页聚合
- `web/src/features/managed-agents/resources/model.tsx`：过滤平台 `MEMORY.md`

测试覆盖 501 拦截、500 原样提交、`read_only`、Session / Deployment 提交体、编辑不回写禁字段、无 store 回归、详情隐藏 `MEMORY.md`、选择器聚合超过一页的 store。
