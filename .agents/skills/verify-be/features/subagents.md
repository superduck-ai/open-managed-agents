# 子智能体委派验证

## 运行

1. 运行 `just verify-be chat doctor subagents`。
2. 运行 `just verify-be chat subagents`。
3. 检查报告中的必需阶段、跳过项和清理结果。

委派运行逻辑改变时，另跑 `chat roundtrip`、`chat tools` 和 `chat public`。
场景使用公开 API、真实 Runner、Docker、FUSE 和 Claude Code；模型响应由脚本提供。
引用 CRUD 与技能版本矩阵由 API 和解析器测试覆盖，不在 Worker 场景重复。

## 必需阶段

- `configured_child_dispatched`：父模型按名称委派，子模型使用启动时固定的定义。
- `child_skill_preloaded`：子模型请求包含上传技能标记。
- `child_stream_received`：子事件流返回正确线程的最终回复。
- `child_tool_policy_verified`：确认前文件不存在，允许后内容正确。
- `child_mcp_executed`：子实例通过会话级 HTTP MCP 服务完成调用。
- `child_thread_history_verified`：事件流与历史的回复一致，结果归属子线程。
- `child_thread_resumed`：原生 `SendMessage` 继续同一实例和线程。

另检查生命周期先于委派事件时的线程路由和会话策略，以及内部回复先于模型代理回复时的去重。
并行场景只验证两个重叠后台实例、各自线程和回复。
缺少阶段、跳过测试、源码变化或清理失败时，不得报告通过。

## 易错边界

- 配置 ID 和版本、运行实例 ID、线程 ID、工具调用 ID 各有用途；续聊使用运行实例 ID。
- 子工具确认携带子线程 ID；原始事件和镜像事件的关联由写入路径生成。
- 成功 HTTP 响应不能代替最终文件、历史和线程状态证据。
- 会话完成后先归档，再清理测试沙箱；清理失败必须出现在报告中。

## 未覆盖范围

真实模型推理、定向中断、子线程重启恢复、云端分配和完整隧道服务未覆盖。
浏览器展示、文件系统隔离和技能隔离不属于该后端场景。
会话级隧道配置与 OAuth 地址由处理器测试覆盖。
配置、身份和权限规则见[子智能体委派设计](../../../../docs/design/be/subagent-delegation.md)。
