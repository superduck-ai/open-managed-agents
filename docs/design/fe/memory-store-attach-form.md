# Memory Store Attach 表单

## 目标

Create Session 与 Create Deployment 可以选 Memory Store、Access 和 Instructions。提交体只带客户端允许的字段；服务端 `mount_path` / `name` / `description` 可以只读展示，不能回写。

后端 attach 合同见 [memory-store.md](../be/memory-store.md)。本文只描述控制台表单。

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
- 不发送 `mount_path`、`name`、`description`。编辑已有 Deployment 时，即使 GET 响应带有这些字段，仅修改其他配置时 POST 省略 `resources`；选择 Replace resources 后，Memory Store 项也只回写上面四个允许字段。
- Session 的 `resources` 为文件资源、Git 仓库资源后接 memory 资源。未选 store 时不出现 memory 项。

## 表单

Create Session 的 Resources 与官方控制台一致：`+ Resource` 菜单里选 Memory store，生成一张卡片（选择 store、Access 下拉、可选 Instructions）。Access 为 `read_write` / `read_only`，文案为 Read & write / Read only。Instructions 标签旁标 (optional)，输入框占位为 “Tell the agent what this store contains and when to use it.”。Instructions 按 Unicode 码点计数，上限 500；超过时禁用提交，不截断。服务端 `mount_path` 仅在编辑已有挂载时只读展示。Store 选择器通过 `GET /v1/memory_stores?beta=true&limit=100&include_archived=false` 加载当前 Workspace 的活动 store；响应存在 `next_page` 时继续请求，直至完整列表。选择器只在已加载选项上按名称或 ID 过滤，不另做服务端搜索。

## 实现与验收

- `web/src/features/managed-agents/resources/memory-attach.ts`：组包、500 码点、禁字段剥离
- `web/src/features/managed-agents/resources/MemoryStoresAttachField.tsx`：Memory store Resource 卡片
- `web/src/features/managed-agents/resources/ManagedResourceFields.tsx`：统一资源菜单（File / Git repository / Memory store）及 Deployment 整组替换流程
- `web/src/features/managed-agents/sessions/SessionFileResourcesField.tsx`：文件资源卡片
- `web/src/features/managed-agents/sessions/file-resource-form.ts`：纯表单校验与 File resource 序列化；创建 Session 和追加文件资源复用同一组包函数，提交就绪判断不依赖 UI 组件。
- `web/src/features/managed-agents/api.ts`：Session 与 Deployment 创建、更新请求体；Store 选择器分页聚合

测试覆盖 File / Git / Memory 组合提交、Deployment 未修改资源时省略字段、501 拦截、500 原样提交、`read_only`、Session / Deployment 提交体、编辑不回写禁字段、无 store 回归、选择器聚合超过一页的 store。
