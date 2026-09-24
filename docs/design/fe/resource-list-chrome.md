# 资源列表页头、空态与未找到

Environments、Vaults、Memory、Deployments、Sessions、Agents、Skills、Files、Members、Organization、API keys 和 Webhooks 使用同一套列表外观。页头、空态和未找到只对齐外观，不改这些控件的路由和接口。列表分页见下文。

## 页头与表格

- 标题 `28px` / `font-semibold`，说明 `15px` / `leading-5`。宽屏主按钮在右上角，高度 `h-9`。窄屏页头改成纵向排列，按钮落到标题下面，避免和标题重叠。
- 标题和筛选之间 `mb-5`，筛选和表格之间 `mb-7`。没有筛选的列表把页头间距设为 `mb-7`，使标题到表格的距离与筛选到表格一致。
- 表格只有行分隔，没有外层圆角边框，也不再使用卡片标题或图标井。

## 空态与创建弹窗

- 空列表是居中图标、标题和一句话。这句话的动词与页头主按钮相同，空态不再放第二个按钮。
- 创建弹窗标题 `22px` / `font-semibold`。取消在左，提交在右，提交文案与页头主按钮相同。
- API key 页头、弹窗标题和提交都是 “Create API key” / “创建 API 密钥”。Webhook 三处都是 “Create webhook endpoint” / “创建 Webhook 端点”。邀请三处都是 “Invite” / “邀请”。

## 列表分页与删除补位

控制台资源列表共用 `consoleResourceListLimit`，每页 10 条：Sessions、Agents、Deployments、Environments、Vaults、Memory、Files、Skills 和 Batches。Sessions、Deployments、Environments、Vaults 和 Memory 经 `listManagedEntities` 带上这个 `limit`；Agents、Files、Skills 和 Batches 使用同一个常量。接口用 `page` / `next_page` 游标翻页，Files 和 Batches 用 `after_id` / `before_id`。这些列表响应都带 `total_count`，计数忽略当前游标，按同一筛选条件统计全部匹配行。翻页条统一为同一套带边框的 `outline` / `icon-lg` 按钮，边框用 `border-foreground/30`。上一页或下一页禁用时，边框粗细和颜色保持不变，只把箭头改成浅色，避免有的列表看起来没有框。按钮中间显示当前页和总页数，例如 `1 / 2`。当前页来自客户端游标历史或 `pageIndex`；总页数是 `ceil(total_count / 10)`，空列表显示 `1 / 1`。Agents 搜索把已加载结果按同样的页大小在本地切片，总页数按已加载条数计算。省略 `limit` 时服务端默认仍是 20，上限 1000。删除成功后，以及归档会让该行离开当前查询时，用同一游标重新请求当前页，把下一行补进空位。Agents 的默认 Active 列表在归档后同样重取当前页。Skills 删除后会让当前页查询失效并重取。Files 和 Batches 列表没有删除。

## 未找到与工作区名称

- Agent 和 Session 不存在时使用同一块：标题、一句包含资源 ID 的说明、返回列表的链接。文案跟随当前语言。版本不存在和操作失败仍使用 alert。
- 这些页面上的工作区显示名如果正好是 `Default`，中文界面显示“默认”。批处理空态不在本次范围内。
- 成员说明、表头、角色，组织设置字段和 API 密钥开关，侧边栏工作区显示名与账号角色，以及 Skills、Webhooks 里走文案表的句子，都跟随当前语言。组织 ID 和工作区 ID 保持原值。成员说明里的组织名保持用户保存的名称，包括名为 `Default` 的组织；只有缺少组织名时才回退到工作区显示名。
