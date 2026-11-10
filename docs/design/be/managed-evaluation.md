# 评测集

状态：控制台可以创建、导入和维护评测集。评分标准和评测任务不在这一版。

## 数据

`00117_evaluation_datasets.sql` 不建外键。租户条件同时带 `organization_uuid` 和 `workspace_uuid`。

| 表 | 前缀 | 作用 |
| --- | --- | --- |
| `evaluation_datasets` / `evaluation_dataset_versions` | `evalds_` / `edv_` | 评测集和版本。当前写入版本 1 |
| `evaluation_cases` | `evalcase_` | `query` 必填。期望回答、工具轨迹、来源会话、标注和验收要点可选 |

删除评测集或用例是软删除。

## 控制台

路由在 `/api/console/organizations/{orgUuid}/workspaces/{workspaceId}/evaluations`。

- `POST/GET /datasets`，`GET/DELETE /datasets/{id}`
- `GET/POST /datasets/{id}/cases`，`POST /datasets/{id}/cases/{caseId}`，`DELETE /datasets/{id}/cases/{caseId}`
- `POST /datasets/{id}/cases:import`，`POST /datasets/{id}/cases:generate`
- `GET /datasets/{id}/session-turns?session_id=`，`POST /datasets/{id}/cases:from-session`

前端入口在托管智能体的「评测」。页面只有评测集。

新建评测集可以上传或粘贴 JSON、JSONL、CSV，并下载样例。文件内容随创建请求一次写入；解析失败时不会留下空评测集。详情页列出用户问题、期望回答和按行保存的验收要点。标注分不标注、正例和负例；只有负例按条填写验收要点。编辑用例时未提交的工具轨迹保持原值。名称、说明、问题和验收要点按字符计数。

评测集列表按 15 条分页。从会话导入时先选择当前工作区的会话，选择器可以继续加载后续会话，再把轮次从左侧移到右侧。一轮从一条用户消息开始，到下一条用户消息之前。Agent 的文字回复只用来辨认该轮，不写入期望回答。每个选中的轮次各成为一条用例。轮次移到右侧之后再选标注；只有负例填写验收要点，并应用到这次导入的全部用例。超过 2000 个事件的会话拒绝导入。

生成用例接受 1 到 50 条，可以带补充说明、Agent 和该 Agent 的版本。指定版本时使用该版本的系统提示词。模型返回用例 JSON 数组；`{"cases":[...]}` 也会被接受。生成失败时删除刚创建的空评测集。
