# Session 详情页：工具审批 / AskUserQuestion 接手说明

后端已经能把用户确认写回 Claude Code。控制台 Session 详情页还不能让人点 Allow/Deny，也不能填问卷。请在 Session 详情页把这条交互补上。

相关后端已推到 `cursor/ask-user-question-updated-input`（提交 `dbdfd896`）。前端可以从 `main` 或该分支继续。

## 要做什么

Session 进入 `requires_action` 后，用户必须能在详情页完成动作，而不是在聊天框里发 `user.message`。

有两类阻塞，走**同一个** API：`POST /v1/sessions/{id}/events` 发 `user.tool_confirmation`。

| 阻塞来源 | 识别方式 | UI | 确认 payload |
|---|---|---|---|
| 普通工具审批（Write、MCP 等） | `lifecycle === 'awaiting_approval'`，且工具名不是 `AskUserQuestion` | Allow / Deny | `result` 即可 |
| Claude 问卷 | 工具名是 `AskUserQuestion` | 问卷卡片 | `result=allow`，并带 `answers`（建议同时带回 `questions`） |

当前 Transcript 已经能显示 `awaiting approval` chip，但没有操作按钮。聊天框发的任何文本（包括 JSON）都不会被当成确认。

## 现有设计需要改口

下面两份文档仍写着「Session Detail 只读回放，不要渲染 Allow/Deny，不要发 `user.tool_confirmation`」：

- [session-tool-permission-confirmation.md](./session-tool-permission-confirmation.md) §7、§9
- [session-tool-call-display.md](./session-tool-call-display.md) §2.2
- [session-conversation-workspace.md](./session-conversation-workspace.md)（composer 只发 `user.message` / `user.interrupt`）

这次产品决定改了：控制台 Session 详情页**要**承担审批和问卷提交。落地时请同步改这些文档，并更新 `ManagedAgentsPage.resources.suite.tsx` 里「页面存在 awaiting tool 时不出现 Allow/Deny」的断言。

## API 合同（已可用）

发送入口与现有 composer 相同：`anthropicBetaApi.sessions.events.send`（见 `web/src/features/managed-agents/api.ts` 的 `postQuickstartSessionMessage`）。建议新增一个 `postSessionToolConfirmation`，不要把 confirmation 塞进 `user.message`。

`tool_use_id` 必须是公开 `agent.tool_use` / `agent.mcp_tool_use` 的 `id`（`sevt_...`）。它也出现在 `session.status_idle.stop_reason.event_ids`。不要用事件里的内部 `tool_use_id`（`tool_...` / `toolu_...`）。子线程 cross-post 过来的阻塞事件如有 `session_thread_id`，原样带回。

### 普通审批

```json
{
  "events": [
    {
      "type": "user.tool_confirmation",
      "tool_use_id": "sevt_...",
      "result": "allow"
    }
  ]
}
```

拒绝时：`"result": "deny"`，可选 `"deny_message": "..."`。

### AskUserQuestion

```json
{
  "events": [
    {
      "type": "user.tool_confirmation",
      "tool_use_id": "sevt_...",
      "result": "allow",
      "updated_input": {
        "questions": [
          {
            "header": "Color",
            "question": "Which verification color should I use?",
            "options": [{ "label": "Blue" }, { "label": "Green" }]
          }
        ],
        "answers": {
          "Which verification color should I use?": "Blue"
        }
      },
      "answers": {
        "Which verification color should I use?": "Blue"
      }
    }
  ]
}
```

`answers` / `updated_input` 是本仓库扩展。后端规则：

- 只在 `result=allow` 时生效；deny 忽略这两项。
- 有非空 `updated_input` 对象就整份替换原始 tool input。
- 有 `answers` 对象再写入 `updatedInput.answers`。
- 两者都缺省时，Write/Bash 行为不变（回传原始 input）。
- 字段一旦出现，必须是 JSON object，不能是数组或字符串。

只发 `answers` 也可以：后端会把它叠到原始 `questions` 上。推荐同时带回原始 `questions`，对齐 Claude Code `canUseTool` 合同。

官方对照：[Handle approvals and user input](https://code.claude.com/docs/en/agent-sdk/user-input)。

## 问卷答案格式（容易写错）

`AskUserQuestion` 的 `input.questions[]` 字段：

| 字段 | 用途 |
|---|---|
| `question` | 完整问题文本。展示用，也是 `answers` 的 **key** |
| `header` | 短标签（最多约 12 字符）。只适合当标题，**不能**当 answers key |
| `options[].label` | 选项文案。这是 answers 的 **value** |
| `options[].description` | 可选说明 |
| `options[].preview` | 可选 HTML 预览 |
| `multiSelect` | 多选时 value 用 label 数组，或用 `", "` 拼成字符串 |

错误示例：`{"Color": "Blue"}`（用了 header）。Claude 会得到空结果 `User has answered your questions: .`。

正确示例：`{"Which verification color should I use?": "Blue"}`。成功时 tool result 类似：

```text
User has answered your questions: "Which verification color should I use?"="Blue".
```

自由回复（用户不选题、直接打一段话）才用 `updated_input.response`；设了 `response` 后 Claude 收到的是 “The user responded: …”，而不是逐题答案。

## 不要复用 Quickstart 问卷卡片

`web/src/features/managed-agents/quickstart/questions/AskUserQuestionsCard.tsx` 服务的是 Quickstart 自定义工具 `ask_user_questions`，提交时走 `onCompleteTool`，payload 是：

```json
{ "answers": [{ "header": "...", "question": "...", "answers": ["Blue"] }] }
```

这和 Claude Code 的 `answers` map **不是同一种合同**。可以复用：

- `web/src/shared/ui/questionnaire`（shadcn Questionnaire）
- `parseQuestionInput()`（`web/src/features/managed-agents/quickstart/questionModel.ts`）
- 现有 i18n / 确认按钮样式

不要直接挂 `AskUserQuestionsCard`。

## 建议落点

| 文件 | 作用 |
|---|---|
| `web/src/features/managed-agents/sessions/SessionDetailPage.tsx` | 从当前 transcript entries 找出 `awaiting_approval` 的 tool call |
| `web/src/features/managed-agents/sessions/SessionMessageComposer.tsx` | 等待确认时不要让用户误发 `user.message`；问卷/审批区可放在 composer 上方 |
| `web/src/features/managed-agents/sessions/sessionTraceModel.ts` | 已有 `sessionToolLifecycle`、`awaiting_approval`；优先复用，不要另写一套状态机 |
| `web/src/features/managed-agents/sessions/sessionTraceRows.tsx` / `SessionTracePanel.tsx` | 行内 chip 已有；按钮可以放行内，也可以放 composer 区 |
| `web/src/features/managed-agents/api.ts` | 新增 confirmation 发送函数 |

实现顺序建议：

1. 普通工具 Allow/Deny（Write 最快能测到）。
2. `AskUserQuestion` 问卷 + 按 `question` 文本组 `answers`。
3. 等待确认时禁用或提示 composer，避免用户把答案打进聊天框。
4. 更新上面列出的设计文档和现有「禁止 Allow/Deny」测试。

控件用 shadcn（`Button`、`Questionnaire`），不要手写一套。

## 怎么验收

本地后端 `just restart-server`（`127.0.0.1:38080`），改了 `web/` 后 `just restart-web`。

1. Agent 把目标工具（至少 Write）设成 `always_ask`。
2. 新开 Session，让 agent 写文件。页面应停在 `awaiting approval`，点 Allow 后 Write 继续跑出 result。点 Deny 应看到 `denied`，agent 收到拒绝说明。
3. 让 agent 调用 `AskUserQuestion`（例如「用 AskUserQuestion 问我选 Blue 还是 Green」）。选一项并确认后，tool result 必须带上问题文本和选项 label，不能是空的 `User has answered your questions: .`。
4. 聊天框里输入 JSON / 选项文字，不应推进阻塞工具。

测审批不要用只读 Bash（`printf`、`ls`）。Claude Code default 模式会在本地放行，**不会**发 `can_use_tool`，Session 不会进入 `requires_action`。

## 后端参考

- 桥接：`internal/codesessions/tool_permissions.go` 的 `toolConfirmationUpdatedInput`
- 校验：`internal/sessions/service_helpers.go`（`updated_input` / `answers` 必须是 object）
- 设计：`docs/design/be/managed-agent-claude-code-permission-bridge.md` §4.3
- 契约：`docs/design/be/permission-policies.md`「响应确认请求」
- API 测试：`tests/sessions_api_test.go` 的 `TestCodeSessionAskUserQuestionConfirmationForwardsUpdatedInput`

前端改动后跑窄范围测试（`ManagedAgentsPage` session 相关 suite）和 `bun run build`。
