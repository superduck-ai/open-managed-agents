import { expect, test } from 'bun:test';
import { costTotalLabel, formatCost, money, reportRange, toolCostDollars, type UsageMetrics } from './usage-api';

test('最近七天跨年仍包含今天且使用 UTC 日界线', () => {
  expect(reportRange('last_7_days', new Date('2026-01-03T23:30:00Z'))).toEqual({
    start: '2025-12-28T00:00:00.000Z',
    end: '2026-01-04T00:00:00.000Z',
  });
});
test('月底的本月至今不会跳到下个月', () => {
  expect(reportRange('month_to_date', new Date('2026-01-31T23:30:00Z'))).toEqual({
    start: '2026-01-01T00:00:00.000Z',
    end: '2026-02-01T00:00:00.000Z',
  });
});
test('总费用等于 Token、工具调用和会话运行成本之和', () => {
  expect(costTotalLabel('0', '0', '0.025451', 'en')).toBe('$0.025451');
  expect(costTotalLabel('1.25', '0.02', '0', 'en')).toBe('$1.270000');
  expect(formatCost('0.001388888', 'en')).toBe('$0.001389');
  expect(money('125', 'en')).toBe('$1.250000');
  expect(money('0', 'en')).toBe('$0.000000');
});
test('工具调用费用目前只合计网页搜索', () => {
  const metrics = { search_cost_cents: '2', search_cost: '0.001389' } as UsageMetrics;
  expect(toolCostDollars(metrics)).toBe('0.001389');
  expect(toolCostDollars({ search_cost_cents: '2' } as UsageMetrics)).toBe('0.02');
});
test('会话对账旧月份链接仍覆盖完整月份', () => {
  expect(reportRange('2024-02')).toEqual({
    start: '2024-02-01T00:00:00.000Z',
    end: '2024-03-01T00:00:00.000Z',
  });
});
