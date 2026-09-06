import { describe, expect, test } from 'bun:test';
import { cronToSchedule, previewSchedule, scheduleToCron } from './deployment-schedule';

const now = new Date('2026-09-06T12:00:00Z');

describe('deployment schedule', () => {
  test('rejects unsupported syntax, invalid dates and timezones', () => {
    for (const cron of ['', 'bad', '0 0 0 * * *', '0 9 ? * *', '0 0 L * *', '@daily', '60 9 * * *', '0 0 31 2 *']) {
      expect(previewSchedule(cron, 'UTC', now).error).toBe('cron');
    }
    for (const timezone of ['', 'Local', 'not/a-zone']) {
      expect(previewSchedule('0 9 * * *', timezone, now).error).toBe('timezone');
    }
    expect(scheduleToCron({ frequency: 'daily', time: '', day: '1' })).toBe('');
    expect(scheduleToCron({ frequency: 'daily', time: '25:00', day: '1' })).toBe('');
  });

  test('preserves custom expressions instead of guessing a graphical frequency', () => {
    expect(cronToSchedule('*/15 9-17 * * 1-5').frequency).toBe('custom');
  });

  test('round trips all graphical frequencies and Sunday alias', () => {
    for (const expression of ['* * * * *', '15 * * * *', '30 8 * * *', '0 9 * * 1-5', '45 17 * * 0']) {
      expect(scheduleToCron(cronToSchedule(expression))).toBe(expression);
    }
    expect(scheduleToCron(cronToSchedule('0 9 * * 7'))).toBe('0 9 * * 0');
  });

  test('previews the next five weekdays in the selected timezone', () => {
    const preview = previewSchedule('0 9 * * 1-5', 'Asia/Shanghai', now);
    expect(preview.error).toBeNull();
    expect(preview.runs.map((date) => date.toISOString())).toEqual([
      '2026-09-07T01:00:00.000Z',
      '2026-09-08T01:00:00.000Z',
      '2026-09-09T01:00:00.000Z',
      '2026-09-10T01:00:00.000Z',
      '2026-09-11T01:00:00.000Z',
    ]);
  });

  test('skips nonexistent spring times and includes both fall offsets', () => {
    expect(
      previewSchedule('30 2 * * *', 'America/New_York', new Date('2026-03-08T00:00:00Z')).runs[0].toISOString(),
    ).toBe('2026-03-09T06:30:00.000Z');
    expect(
      previewSchedule('30 1 * * *', 'America/New_York', new Date('2026-11-01T00:00:00Z'))
        .runs.slice(0, 2)
        .map((date) => date.toISOString()),
    ).toEqual(['2026-11-01T05:30:00.000Z', '2026-11-01T06:30:00.000Z']);
    expect(
      previewSchedule('30 1 * * *', 'America/New_York', new Date('2026-11-01T05:45:00Z')).runs[0].toISOString(),
    ).toBe('2026-11-01T06:30:00.000Z');
  });

  test('orders frequent runs across a repeated hour and supports leap days beyond one year', () => {
    const frequent = previewSchedule('* * * * *', 'America/New_York', new Date('2026-11-01T05:58:00Z'));
    expect(frequent.runs.map((date) => date.toISOString())).toEqual([
      '2026-11-01T05:59:00.000Z',
      '2026-11-01T06:00:00.000Z',
      '2026-11-01T06:01:00.000Z',
      '2026-11-01T06:02:00.000Z',
      '2026-11-01T06:03:00.000Z',
    ]);
    expect(previewSchedule('0 9 29 2 *', 'UTC', now).runs[0].toISOString()).toBe('2028-02-29T09:00:00.000Z');
  });
});
