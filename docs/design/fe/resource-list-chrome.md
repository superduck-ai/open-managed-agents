# 资源列表页头、空态与未找到

Environments、Vaults、Memory、Deployments、Sessions、Agents、Skills、Files、Members、Organization、API keys 和 Webhooks 使用同一套列表外观。行为、路由和接口不变。

## 页头与表格

- 标题 `28px` / `font-semibold`，说明 `15px` / `leading-5`。宽屏主按钮在右上角，高度 `h-9`。窄屏页头改成纵向排列，按钮落到标题下面，避免和标题重叠。
- 标题和筛选之间 `mb-5`，筛选和表格之间 `mb-7`。没有筛选的列表把页头间距设为 `mb-7`，使标题到表格的距离与筛选到表格一致。
- 表格只有行分隔，没有外层圆角边框，也不再使用卡片标题或图标井。

## 空态与创建弹窗

- 空列表是居中图标、标题和一句话。这句话的动词与页头主按钮相同，空态不再放第二个按钮。
- 创建弹窗标题 `22px` / `font-semibold`。取消在左，提交在右，提交文案与页头主按钮相同。
- API key 页头、弹窗标题和提交都是 “Create API key” / “创建 API 密钥”。Webhook 三处都是 “Create webhook endpoint” / “创建 Webhook 端点”。邀请三处都是 “Invite” / “邀请”。

## 未找到与工作区名称

- Agent 和 Session 不存在时使用同一块：标题、一句包含资源 ID 的说明、返回列表的链接。文案跟随当前语言。版本不存在和操作失败仍使用 alert。
- 这些页面上的工作区显示名如果正好是 `Default`，中文界面显示“默认”。批处理空态不在本次范围内。
- 成员说明、表头、角色，组织设置字段和 API 密钥开关，侧边栏工作区显示名与账号角色，以及 Skills、Webhooks 里走文案表的句子，都跟随当前语言。组织 ID 和工作区 ID 保持原值。成员说明里的组织名保持用户保存的名称，包括名为 `Default` 的组织；只有缺少组织名时才回退到工作区显示名。
