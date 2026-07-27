# Session File Resource 表单

## 目标

控制台补齐 Session 输入文件的最短操作路径：

1. 在当前 Workspace 的 Files 页面上传文件并取得 `file_...` ID；
2. 在 Create Session 对话框添加 File Resource；
3. 提交现有 `POST /v1/sessions` 的 `resources` 字段。

后端的 filesystem、输入引用、只读挂载和 Files API 投影统一由 [Filestore 设计](../be/filestore.md)定义，本文只描述前端接口。

## Files 上传

Files 页面使用现有 Anthropic Files client 上传一个或多个文件：

```text
POST /v1/files?beta=true
multipart/form-data
X-Workspace-ID: <当前 Workspace>
```

全部成功后刷新列表；任一请求失败时显示错误 toast。多文件并发上传可能部分成功，当前实现不回滚，也不会在失败分支自动刷新列表。

## Create Session 表单

Resources 区域可以添加多张 File 卡片。每张卡片包含 File ID、Mount path、`Manage files` 链接和删除按钮。

前端、API 与 Sandbox 使用三种路径表示：

```text
表单:     /uploads/reports/input.csv
API:      /reports/input.csv
Sandbox:  /mnt/session/uploads/reports/input.csv
```

提交时前端去掉固定 `/uploads` 前缀：

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

`source` 不由前端发送，后端默认为 `/uploads`。

## 校验

前端只做即时校验：

- File ID 去除首尾空白后非空；
- Mount path 以 `/uploads/` 开头；
- 不以 `/` 结尾，不包含 `//`、`.` 或 `..` 路径段。

File 是否存在、Workspace 隔离、100 个上限、跨卡片路径冲突、Filestore namespace 冲突和完整路径规则均以后端为权威。

## 实现与验收

主要落点：

- `web/src/features/dashboard/files.tsx`：上传入口与反馈；
- `web/src/features/managed-agents/sessions/SessionFileResourcesField.tsx`：Resource 卡片；
- `web/src/features/managed-agents/sessions/file-resource-path.ts`：路径转换；
- `web/src/features/managed-agents/api.ts`：Create Session 请求体。

测试覆盖上传成功/失败、Workspace header、空资源、路径转换、非法输入禁用创建、删除草稿卡片、Files 链接和运行时路径预览。
