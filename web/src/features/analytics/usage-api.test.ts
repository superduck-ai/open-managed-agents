import { expect, test } from 'bun:test';
import { reportRange } from './usage-api';

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
test('会话对账旧月份链接仍覆盖完整月份', () => {
  expect(reportRange('2024-02')).toEqual({
    start: '2024-02-01T00:00:00.000Z',
    end: '2024-03-01T00:00:00.000Z',
  });
});
