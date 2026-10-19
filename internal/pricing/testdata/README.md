# Narrows 人民币价格夹具

- 来源：https://narrows.seewo.com/pricing/api/rules
- 获取日期：2026-10-19，共 61 个模型。
- 固定汇率：1 USD = 7.2 CNY。
- `narrows.json` 保留价格源响应，不包含调用凭据。
- `narrows-expected.json` 为独立 Python Decimal 复算结果：每模型覆盖基础输入及各分档阈值前、等于、后，输出固定 1000 Token。按原始每 Token 单价求和、除以 7.2、乘 100 后使用 ROUND_HALF_UP 得到美分，共 302 组。
- 缺少输出价格，或需要区分思考模式/图像输出的用例，预期金额为 null；不得套用普通输出价格。
