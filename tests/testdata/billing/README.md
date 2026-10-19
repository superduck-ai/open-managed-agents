# 多模型计费实测

价格快照、测试数据、报告和截图仅保存在本地 `tmp/`，不纳入 Git。
离线回归 `TestModelPriceReconciliation` 使用合成价格和固定预期值，覆盖精确及短名匹配、
未定价、缓存和分档阈值，贯通隔离 PostgreSQL、流式/非流式计量、账本及每日汇总。
Narrows 解析单测使用合成规则覆盖汇率、分档上下界和无法区分的价格类别。

`TestLiveNarrowsBilling` 默认跳过，只有显式设置下列环境变量才会调用真实上游并产生费用：

- `LIVE_BILLING_BASE_URL`：已启动的测试服务。
- `CONFIG_FILE`：该服务的数据库配置，仅用于读取验收账本；不会执行迁移或重置数据库。
- `LIVE_BILLING_PRICES`：本轮下载到本地的完整价格 JSON。
- `LIVE_BILLING_SOURCE`：设为 `narrows` 时使用 Narrows 原规则 JSON，并默认实测其中全部模型；否则使用 OpenRouter 价格 JSON，必须用 `LIVE_BILLING_MODELS` 指定模型，避免意外调用整个目录。
- `LIVE_BILLING_REPORT`：本地脱敏 JSON 报告输出路径，不纳入 Git。
- `LIVE_BILLING_API_KEY`：可选，默认使用项目本地测试 API Key。

每个模型只发一条短文本请求，并发最多 4、单请求超时 45 秒。报告仅保存模型、用量、
价格来源、金额及结果，不保存回复正文或密钥。真实测试不证明供应商实际账单、图像/音频价格、
高输入分档或未实际命中的缓存路径；这些边界必须在报告中单独标明。

较长输入与缓存复测可设置 `LIVE_BILLING_MODELS`（逗号分隔的子集）和
`LIVE_BILLING_INPUT_REPEATS`（0–30000）。保持相同参数重复执行，保存不同报告。
通过 `LIVE_BILLING_CACHE_BASELINE` 与 `LIVE_BILLING_CACHE_CANDIDATE` 指定这两轮报告，
运行 `TestLiveBillingCacheComparison` 检查缓存命中前后的总输入是否守恒。
两份报告必须来自相同请求正文及配置；这一检查是异常诊断，不能替代供应商账单核验。
2026-10-18 的真实 GPT-5.2 样例已触发该检查，不能将“原始字段计算与落账相同”当作最终费用正确。
真实请求中的任意阻塞或对账不一致都会使验收测试失败，报告保留每个模型的独立结果。
