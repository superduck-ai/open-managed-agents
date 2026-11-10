# 评测集

状态：控制台可以创建、导入和维护评测集，也可以手写或生成评分标准。评测任务不在这一版。

## 数据

`00117_evaluation_datasets.sql` 不建外键。租户条件同时带 `organization_uuid` 和 `workspace_uuid`。

| 表 | 前缀 | 作用 |
| --- | --- | --- |
| `evaluation_datasets` / `evaluation_dataset_versions` | `evalds_` / `edv_` | 评测集和版本。当前写入版本 1 |
| `evaluation_cases` | `evalcase_` | `inputs` 是有序内容块。`query` 由其中的文字块拼出，可以是空字符串。期望回答、工具轨迹、来源会话、标注和验收要点可选 |
| `evaluation_rubrics` / `evaluation_rubric_versions` | `evalrb_` / `erv_` | 评分标准。版本只增不改。`00118_evaluation_rubrics.sql` |

删除评测集或用例是软删除。

## 控制台

路由在 `/api/console/organizations/{orgUuid}/workspaces/{workspaceId}/evaluations`。

- `POST/GET /datasets`，`GET/DELETE /datasets/{id}`
- `GET/POST /datasets/{id}/cases`，`POST /datasets/{id}/cases/{caseId}`，`DELETE /datasets/{id}/cases/{caseId}`
- `POST /datasets/{id}/cases:import`，`POST /datasets/{id}/cases:generate`
- `GET /datasets/{id}/session-turns?session_id=`，`POST /datasets/{id}/cases:from-session`
- `POST/GET /rubrics`，`GET/DELETE /rubrics/{id}`，`GET /rubrics/{id}/versions`，`POST /rubrics/{id}/versions`，`POST /rubrics/generate`

前端入口在托管智能体的「评测」。页面有评测集和评分标准。

新建评测集可以上传或粘贴 JSON、JSONL、CSV，也可以上传带单元格图片的 xlsx，并下载样例。上传页写明只有 xlsx 能放图片。JSON、JSONL、CSV 样例只有文字，Excel 样例在单元格里带一张图片。上传和粘贴各自保留内容。文件内容随创建请求一次写入；解析失败时不会留下空评测集。只提供 `query` 时，服务端保存成一个文字块。`inputs` 可以按顺序包含文字、图片和文档。图片和文档只引用当前工作区的 `file_id`，类型与会话消息一致；Base64 和外部 URL 会被拒绝。详情页列出用户问题、期望回答和按行保存的验收要点。问题列在文字下显示图片缩略图，点开查看大图；文档显示文件名。缩略图走控制台预览，读取当前工作区里的图片字节。会话导入的图片会带上用例里的会话，再读取会话附件。公开文件下载接口仍然拒绝不可下载的上传文件。不能预览的文件返回 “File is not previewable”。图片加载失败时显示「图片」。编辑用例使用同样的缩略图，移除按钮在缩略图角上，界面不显示文件编号。标注分不标注、正例和负例；只有负例按条填写验收要点。编辑用例时未提交的工具轨迹保持原值，并只读取这一条用例的轨迹。多行 JSONL 按行导入；`{"cases":[...]}` 也会被接受。xlsx 里单元格图片（`DISPIMG`）和浮动图片由浏览器直接读出字节，上传到文件库后再保存文件编号。用户不填写 `file_id`，也不用按文件名再选一次。CSV 和旧版 xls 没有图片字节，遇到单元格图片公式时提示改传 xlsx。编辑从会话导入的用例时，文件库里已有的文件直接使用。只有文件不在文件库时，才按用例记录的会话读取会话附件。该会话已不存在时，这个附件返回 Session not found。只改文字或文件库文件时，不因会话消失而失败。编辑用例和从会话导入仍然直接使用已经上传或会话里已有的文件。名称、说明、问题和验收要点按字符计数。

评测集列表按 15 条分页。从会话导入时先选择当前工作区的会话，选择器可以继续加载后续会话，再把轮次从左侧移到右侧。一轮从一条用户消息开始，到下一条用户消息之前。可以导入的轮次保留文字，以及这场会话能读到的图片和文档文件。手动添加和 JSON 导入只接受文件库里可见的 `file_id`。只有 Base64 或 URL 的轮次不出现在选择器里；如果导入请求仍指向这样的轮次，该轮次计入 `skipped`，不写入用例。已经导入成功的轮次会从可选列表移除，避免再次导入变成重复用例。导入进行中不能更换会话或轮次，完成后按提交时的会话和轮次标记已导入。Agent 的文字回复只用来辨认该轮，不写入期望回答。每个选中且可导入的轮次各成为一条用例。轮次移到右侧之后再选标注；只有负例填写验收要点，并应用到这次导入的全部用例。超过 2000 个事件的会话拒绝导入。

生成用例接受 1 到 50 条，可以带补充说明、Agent 和该 Agent 的版本。指定版本时使用该版本的系统提示词。模型返回用例 JSON 数组；`{"cases":[...]}` 也会被接受。外层 Markdown 代码围栏会先去掉。模型输出无法解析时按生成失败返回。生成失败时删除刚创建的空评测集。

评分标准可以手写，或按评测目标、Agent、评测集、正例和负例生成。生成要求模型直接返回 Markdown；若整段回复是 JSON 对象或数组，则拒绝落库。模型若用 Markdown 代码块包住正文，保存时去掉围栏。标题可以留空。列表按 15 条分页。正文和模型名按字符计数。详情先展示标题、编号、版本、创建时间和评分规则正文。版本历史按版本号倒序列出每一版的正文和通过分，可以选择查看。点编辑后再保存，保存会追加版本，已经存在的评测任务不受影响。删除是软删除。
