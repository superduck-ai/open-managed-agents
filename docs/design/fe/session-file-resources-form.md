# Session File Resource 表单

## 目标

控制台补齐 Session 输入文件的最短操作路径：

1. 在当前 Workspace 的 Files 页面上传文件；
2. 在 Create Session 对话框添加 File Resource，并从当前 Workspace 的文件列表中查询选择；
3. 提交现有 `POST /v1/sessions` 的 `resources` 字段。

后端的 filesystem、输入引用、只读挂载和 Files API 投影统一由 [Filestore 设计](../be/filestore.md)定义，本文只描述前端接口。

## Files 上传

Files 页面使用现有 Anthropic Files client 上传一个或多个文件：

```text
POST /v1/files?beta=true
multipart/form-data
X-Workspace-ID: <当前 Workspace>
```

所有上传请求结束后分别统计成功与失败结果。只要至少一个文件上传成功，就使用实际成功数量显示成功 toast，清空当前游标并刷新第一页；任一请求失败时同时显示错误 toast。多文件并发上传可能部分成功，当前实现不回滚。

## Create Session 表单

Resources 区域可以添加多张 File 卡片。每张卡片包含 File 选择器、可选 Mount path、`Manage files` 链接和删除按钮。File 选择器通过 `GET /v1/files?beta=true&limit=1000` 查询当前 Workspace 的文件元数据，支持按文件名或 File ID 过滤，并以“文件名（可读大小）”展示选项；提交时仍只写入所选文件的 `file_id`，不改变现有 Session API 合同。加载中、加载失败和空列表均在选择器弹层中给出明确状态。

前端、API 与 Sandbox 使用三种路径表示：

```text
表单:     reports/input.csv
API:      /reports/input.csv
Sandbox:  /mnt/session/uploads/reports/input.csv
```

Mount path 输入组固定展示 Sandbox 前缀 `/mnt/session/uploads/`，用户只编辑后面的相对路径，无需输入 `/uploads` 或 Sandbox 前缀。填写自定义路径时，提交前端补上 API 所需的开头 `/`：

```json
{
  "resources": [
    {
      "type": "file",
      "file_id": "file_abc123",
      "mount_path": "/reports/input.csv"
    }
  ]
}
```

Mount path 留空时，前端不发送 `mount_path`；服务端按既有合同默认使用 `/uploads/<filename>`，Sandbox 中对应 `/mnt/session/uploads/<filename>`。

`source` 不由前端发送，后端默认为 `/uploads`。

## 校验

前端只做即时校验：

- 已从当前 Workspace 文件列表中选择 File，且对应 File ID 非空；
- Mount path 可以留空；留空时使用服务端默认文件名；
- 自定义 Mount path 使用相对于 `/uploads` 的路径，不以 `/` 开头或结尾，不包含 `//`、`.` 或 `..` 路径段。

File 是否存在、Workspace 隔离、500 个上限、跨卡片路径冲突、Filestore namespace 冲突和完整路径规则均以后端为权威。

## 实现与验收

主要落点：

- `web/src/features/dashboard/files.tsx`：上传入口与反馈；
- `web/src/features/managed-agents/sessions/SessionFileResourcesField.tsx`：Resource 卡片；
- `web/src/features/managed-agents/sessions/file-resource-path.ts`：路径转换；
- `web/src/features/managed-agents/api.ts`：Create Session 请求体。

测试覆盖上传成功/失败与部分成功、上传后返回第一页、Workspace header、文件列表查询与展示、按文件名过滤和选择、空资源、相对路径转换及非法路径段、非法输入禁用创建、删除草稿卡片、Files 链接和运行时路径预览。
