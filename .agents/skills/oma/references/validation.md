# 中文技能验收记录

北京时间 2026-11-05，目标 `https://oma.gz.cvte.cn`，原版 ant 2.5.0。这里记录的是直接执行 CLI 与 API 的真实结果，不代表独立模型已自动完成整套 Skill，也不代表全域集成回归。

先测试失败路径，再测试成功路径。密钥未保存到仓库，未运行 verify-be；本次验证客户端 Skill，不修改服务端。

| 场景 | 结果与判断依据 |
| --- | --- |
| 无效 key | HTTP 401 |
| 不存在的 Agent | HTTP 404 |
| 错误版本更新 | HTTP 409，随后有效更新到 v2 |
| 查询模型 | ant 输出有效列表 |
| 中文文件配置 dry-run | 生成计划，不创建资源 |
| 中文 Agent apply 创建 | 创建 v1，保存实际 ID |
| 重复 apply | 无变更，不重复创建 |
| limited 环境创建 | HTTP 200，响应保留 limited 与空允许列表 |
| 中文首轮 | 回复“中文接入成功”，最终 idle/end_turn |
| 同 Session 第二轮 | 准确回复上一轮口令“青竹”，最终 idle/end_turn |
| 两页事件查询 | next_page 有值；第二页无第一页的重复 ID |
| Agent 配置更新 | apply 后 Agent v2，既有 Session 仍为 v1 快照 |
| 文件上传 | 返回 file ID、25 字节与 downloadable=false |
| 上传文件下载 | HTTP 400，未通过，不提供已通过承诺 |
| 测试文件删除 | ant delete 成功 |

本次保留的中文测试资源：Agent `agent_wtoikXWMG3g9djQjBUMm8bf9`、Environment `env_sL1RWEcMWKIm0at9Iw5KjcKy`。Session ID 与回执由执行时返回；保留用于检查，不将这些 ID 作为用户新建任务的默认值。

此前两次纯文本执行均通过。原始 HTTP SSE 能收到事件，而 ant stream 未输出；环境 API 的 beta 查询差异仍存在，因此技能采用 curl 操作环境和 ant 查询事件历史。

## 仍需单独验收

- 受限网络对具体主机的阻断效果。
- 工具执行与人工确认、MCP、Vault、Memory、Deployment。
- Agent 生成文件的可下载交付。
- 超时恢复、并发冲突恢复、跨实例隔离与完整 SDK 集成。
- 不同模型、ant 版本与操作系统。

URL 解析、凭据隔离目前是 Skill 指令合同，没有可执行配置解析器；结构校验不能证明这些规则由所有宿主自动执行。连接示例依赖 Bash、ant、curl；读取 lockfile 示例另外需要 jq。发布前保持中文说明、真实通过项和限制一致。
