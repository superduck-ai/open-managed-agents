# 控制台文档与接入示例

产品帮助入口统一调用 `web/src/shared/documentation.ts` 的 `documentationHref`，使用 OMA 文档站 `https://oma.mintlifysite.com/docs`，随界面语言选择 `zh` 或 `en`。API 协议、SDK 包名和明确标注来源的兼容性参考仍保留 Anthropic 标识。

## 入口映射

| 控制台入口 | OMA 文档路径 |
| --- | --- |
| Agent 多智能体、Skills、Tools | `multiagent-orchestration`、`skills`、`tools` |
| 环境列表、新建环境 | `environments` |
| 环境标准镜像 | `cloud-sandboxes-reference` |
| 环境 API 预览 | `api/environments/get-environment` |
| Workbench API、客户端工具、示例 | `api/messages/create-a-message` |
| 缓存帮助 | `api/messages/create-a-message` |

对话构建助手与旧版 Quickstart 请求都在运行时适配行为提示词：多智能体说明指向当前语言的 OMA 文档；控制台链接使用当前部署的 `/dashboard`。捕获的官方请求快照仍作为协议参考保留，不手工修改生成文件。当前对话构建助手只复用其中经过适配的行为块，不将完整官方 API 参考块作为自身能力来源。

## Workbench 导出

导出提供 Python、TypeScript、cURL 三种 OMA 接入示例，使用当前控制台的同源 `/v1/messages` API 和独立的 `OMA_API_KEY` 环境变量，保留当前草稿的模型与请求内容。导出不读取、复制登录 Cookie 或已有密钥。Bedrock、Vertex 直连选项不属于 OMA 接入流程，已移除。对外部署须将 `/v1` 反向代理到 OMA 后端，与控制台现有 API 访问方式一致。

API 页面使用 OpenAPI 的 `x-mint.href` 发布路径；`api/resources/messages` 和 `api/resources/environments` 虽有 MDX 源码，目前未发布到同名路径，线上返回 404，不作为控制台跳转目标。

## 文档缺口

以下主题尚无专门的中英文产品指南；本次使用现有 Messages 文档作为相关入口，不创建不存在的链接：

- Workbench 完整使用指南：提示词版本、变量、工具、附件、Evaluate 与代码导出。
- 示例与少样本提示教程：Workbench 示例如何参与请求，以及与评测用例的区别。
- 提示缓存配置与排障：供应商支持差异、缓存命中和计费；现有 Messages 文档只说明字段。缓存统计页面仍显示未接通，不因修改文档入口而宣称统计功能可用。

既有 Skills 等页面的多语言 SDK 示例仍有默认客户端初始化；完整的跨语言 OMA 示例校准尚待专项整理。SDK 包名和用于本地 Webhook 签名验证的客户端不能按文字匹配机械替换。

## 验证

文档测试校验源码中静态文档入口的目标在中英文目录有 MDX 文件，或由对应语言 OpenAPI 的 `x-mint.href` 定义并注册在导航中；构建助手测试校验实际行为提示词不再引向官方控制台；Workbench 组件测试覆盖 Python、TypeScript、cURL 的部署地址、密钥变量和语言切换。此次不改变服务端 API 合同、鉴权或资源状态机。
