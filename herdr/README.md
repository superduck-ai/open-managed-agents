# Code review 编排

从本仓库的 Herdr pane 中运行：

```sh
python3 herdr/review.py
```

默认审查相对 HEAD 的全部未提交改动，包括暂存、未暂存和未跟踪源码。运行期间请保持工作区稳定。需要 Python 3、Herdr 0.9.3 兼容的 CLI，以及已完成登录的 `grok`、`cckimi`、`codex`。脚本只通过本机现有命令启动 agent，不读取或复制凭据，也不修改 Herdr 配置。

流程如下：

1. 在当前 tab 创建两个后台 pane，分别启动 grok 和 cckimi。grok 使用原生初始提示确认启动，并指定独立 session ID、禁止子 agent；cckimi 通过 `pane run cckimi` 启动，并等待 Herdr 识别到可输入的 Claude Code agent。
2. 并行投递只读 review，等待两者完成，保存各自终端记录。报告使用首尾标记校验；终端读取不完整时，Grok 通过其 `export` 命令恢复当前 session，cckimi 另写临时报告，恢复失败则停止。
3. 确认工作区未变化，汇集两份记录，创建 Codex pane。
4. Codex 合并重复发现，回到代码核实，逐条决定是否需要修复，修复成立的问题并运行仓库要求的检查。

Codex 会修改当前工作区，不自动提交或推送。脚本不会代答审批，也不会自动运行 `verify-be`。cckimi 的权限行为由用户已有的本地包装器决定，review 的只读要求通过提示词约束；脚本会检查 review 前后的工作区内容摘要，但这不构成文件系统隔离。

自定义范围和超时：

```sh
python3 herdr/review.py --scope '审查当前分支相对 main 的全部变更' --timeout 3600000
python3 herdr/review.py --pane w1:p1
```

`--pane` 必须使用当前会话中实际存在的调用 pane ID。若继承的 `HERDR_PANE_ID` 已失效，先用 `herdr agent list` 确认当前 agent 的 pane，再显式传入。

每次运行的记录位于 `tmp/herdr-review/<run_id>/`：

- `state.json` 保存阶段、agent 名称、pane ID 和错误。
- `grok.txt`、`cckimi.txt` 保存原始终端记录，可能包含提示词及工具输出。
- `grok-review.md`、`cckimi-review.md` 保存完整报告，`reviews.md` 汇集两份报告，尚未去重。
- `conclusion.md` 是 Codex 合并并核实后的结论，包含判断、修复和验证结果。
- `codex.txt` 保存 Codex 终端记录。

`finished` 只表示两个 review 和 Codex 回合结束且已产出结论，不代表所有检查通过。以 `conclusion.md` 的实际结果为准。

审批、启动失败、超时或 review 期间代码变化都会中断后续阶段。保留所有创建的 pane，不关闭其他进程，不自动重投 prompt。根据 `state.json` 的名称使用 `herdr agent get` 和 `herdr agent read` 检查现场，在对应 pane 中处理。确认旧任务已停止后再发起新一轮，避免两个 Codex 同时修改工作区。

终端记录最多读取 2000 行，不能保证覆盖超长响应。Grok 的 session 导出保存在 `grok-session.md`；cckimi 按 Herdr skill 的文件恢复流程处理。首尾标记只能检查报告是否截断，不证明审查内容正确或全面，Codex 仍需核实发现并说明未覆盖范围。Grok 若触发 `agent_prompt_stalled`，只在 Herdr 已处于 idle/done 且指定 session 导出含完整最终报告时接受结果，保留原错误记录，不重投任务。

验证脚本逻辑：

```sh
python3 -m unittest discover -s herdr -p 'test_*.py'
```
