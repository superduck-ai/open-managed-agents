# Managed Agent Quickstart Composer 键盘交互

## 范围

本文定义 `/workspaces/{workspace_id}/agent-quickstart` 中首次描述输入框和后续回复输入框的键盘行为。两种输入框共享一致的发送语义，避免用户在 quickstart 的不同阶段切换操作习惯。

## 交互契约

| 操作 | 首次描述输入框 | 后续回复输入框 |
| --- | --- | --- |
| `Enter` | 发送当前内容 | 发送当前内容 |
| `Shift+Enter` | 插入换行 | 插入换行 |

补充约束：

- 输入为空或正在发送时不重复提交。
- 按键处于 IME composition 状态时，`Enter` 只用于确认输入法文本，不发送消息。
- 长按产生的 repeat 事件不得触发重复发送。
- 点击发送按钮与按 `Enter` 使用同一提交路径。

## 实现与验收

首次描述和后续回复都通过 `PromptComposer` 的 `enter` 提交模式实现。回归测试必须分别覆盖 `Shift+Enter` 不提交和 `Enter` 只提交一次，并在修改交互后对真实浏览器键盘事件进行验证。
