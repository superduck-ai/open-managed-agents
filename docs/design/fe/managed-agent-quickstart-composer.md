# Managed Agent Quickstart Composer 键盘交互

## 范围

四步配置的前三步使用明确的表单与按钮，只有最后一步在线测试包含消息 composer。流程、资源恢复和对话布局见 [Quickstart 交互契约](./managed-agent-quickstart-interactions.md)。

## 交互契约

| 操作                       | 在线测试                  |
| -------------------------- | ------------------------- |
| Enter                      | 发送当前内容              |
| Shift+Enter                | 插入换行                  |
| IME composition 或重复按键 | 不发送                    |
| 点击发送                   | 与 Enter 使用同一提交路径 |

内容为空、请求进行中、Agent 运行、等待工具确认或结果未确认时不重复投递。连接未准备好时先等待 SSE 和历史同步，超时保留内容。成功接受消息后清空输入并恢复对话末尾跟随。

回归测试通过页面检查 Shift+Enter 不提交、Enter 只提交一次、第二轮复用 Session 和未知发送不自动重试；浏览器验收补充真实键盘与消息内部滚动。
