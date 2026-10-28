# Session 详情页：工具审批 / AskUserQuestion 实现说明

功能已经实现。当前状态模型、事件合同与验收场景统一维护在
[Session Tool 权限确认与审批交互设计指南](./session-tool-permission-confirmation.md)。

## 不复用 Quickstart 问卷卡片

Quickstart 的 `AskUserQuestionsCard` 提交 `ask_user_questions` 专用数组：

```json
{ "answers": [{ "header": "...", "question": "...", "answers": ["Blue"] }] }
```

Session 的 `AskUserQuestion` 采用 DeepSeek 参数定义，提交
`{answers:[{id,selected,custom?}]}`，通过 `user.custom_tool_result.content` 返回。
实现只复用 `parseQuestionInput()` 与共享 `Questionnaire` 控件，不直接挂载 Quickstart 卡片。
每题必须携带 `id`，多选使用 `multi_select`；不再支持旧 Claude Code 按问题文本编码的答案。
