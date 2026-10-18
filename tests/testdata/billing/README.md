# Narrows 模型的 OpenRouter 对账夹具

`narrows-openrouter.json` 于 2026-10-18 获取，模型清单来自
https://narrows.seewo.com/pricing/api/rules ，价格来自
https://openrouter.ai/api/v1/models 。Narrows 的人民币价格不参与计算。

保留 61 个 Narrows 模型及 32 个可精确匹配的 OpenRouter 原始价格条目。
其余 29 个模型的预期价格为空，不对供应商前缀、日期版本或模型家族做模糊替换。
夹具是固定日期的回归基准，不能用于宣称当前实时价格或真实供应商账单正确。

103 组用量是合成测试输入：基础组总输入 12345、输出 2345；有缓存读取单价时取读取 3456，
有写入单价时取写入 4567，普通输入为总输入减去读取和写入。各模型分档阈值追加
阈值减一、阈值本身和阈值加一三组，验证缓存计入分档总输入且不会重复计费。

预期 `cents` 独立使用原始每 Token 美元价格的十进制运算生成：
逐项 Token 数乘原始单价、求和、乘 100，最后以 ROUND_HALF_UP 舍入为整数美分。
按总输入严格大于阈值选择覆盖价，不调用项目的价格解析器或费用计算器生成预期值。

`TestNarrowsModelsOpenRouterReconciliation` 将原始价格经正式解析器写入隔离 PostgreSQL，
使用正式数据库模型匹配与计量 Transport，验证 206 条流式/非流式路径的快照、用量、费用、
未定价状态与每日汇总。HTTP 上游响应来自测试服务器，不代表真实供应商响应。

`TestLiveNarrowsBilling` 默认跳过，只有显式设置下列环境变量才会调用真实上游并产生费用：

- `LIVE_BILLING_BASE_URL`：已启动的测试服务。
- `CONFIG_FILE`：该服务的数据库配置，仅用于读取验收账本；不会执行迁移或重置数据库。
- `LIVE_BILLING_PRICES`：本轮下载的完整 OpenRouter 价格 JSON。
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
