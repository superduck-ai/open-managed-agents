<!-- 感谢贡献！请先关联对应 issue（如 Closes #28），再按需勾选下方清单。 -->

## 摘要

<!-- 一两句话说明本 PR 做了什么、为什么。 -->

## 变更类型

- [ ] 后端（`internal/**`、HTTP 路由、DB schema、迁移）
- [ ] 前端（`web/**`、路由、UI）
- [ ] CI / 工作流 / 脚本
- [ ] 文档（`docs/design/**` 或 README）
- [ ] 测试
- [ ] 其他

## 自检清单

- [ ] 已关联 issue（如 `Closes #<n>`）
- [ ] `go test ./... -count=1` 通过（后端改动）
- [ ] `bun run build` 通过（前端改动）
- [ ] 新增/修改 schema 已通过 `internal/db/migrations` 中的 goose migration 管理，未修改已应用 migration
- [ ] 新增 HTTP 路由使用 chi 挂载，业务 handler 保持在 `net/http` 边界
- [ ] 多租户查询/写入显式带 `organization_id` / `workspace_id` 等作用域

## 设计文档同步

如果本 PR 改动了 **行为、公开 API、事件契约、状态机、数据模型、权限边界、架构边界或测试/验收路径**，需同步 `docs/design/` 下对应设计文档。

- [ ] 本 PR 无需同步设计文档（纯 CI / 测试 / 配置 / 文档 / 基础设施变更，或受影响 surface 已在 `surface_map.md` 标记 `internal` / `gated:<reason>`）
- [ ] 已更新对应设计文档
- [ ] 需要但尚未同步 —— 触发 docs-sync agent：

  > 在 PR 评论中输入 `@duckpr docs`（运行确定性审计 + LLM 同步）
  > 或 `@duckpr docs --audit-only`（仅确定性审计，不调用 LLM）

<!-- design-doc-audit CI 会在检测到覆盖缺失时自动提示；此清单用于作者提前自检。 -->
