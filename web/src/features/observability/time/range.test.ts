import { expect, test } from 'bun:test';
import {
  formatAbsolute,
  parseTime,
  rangeProblem,
  refreshMs,
  resolveRange,
  shiftRange,
  storedText,
  zoomOutRange,
} from './range';

const NOW = Date.UTC(2026, 9, 27, 13, 45, 30);

test('rejects malformed expressions, reversed ranges and ranges outside one minute to one year', () => {
  expect(parseTime('now-3x', NOW, 'utc')).toBeNull();
  expect(parseTime('2026-02-30 00:00', NOW, 'utc')).toBeNull();
  expect(parseTime('yesterday', NOW, 'utc')).toBeNull();
  expect(rangeProblem({ from: 'nope', to: 'now' }, NOW, 'utc')).toBe('invalid');
  expect(rangeProblem({ from: 'now', to: 'now-1h' }, NOW, 'utc')).toBe('order');
  expect(rangeProblem({ from: 'now-30s', to: 'now' }, NOW, 'utc')).toBe('tooShort');
  expect(rangeProblem({ from: 'now-367d', to: 'now' }, NOW, 'utc')).toBe('tooLong');
  expect(resolveRange({ from: 'now-367d', to: 'now' }, NOW, 'utc')).toBeNull();
});

test('parses relative offsets with calendar rounding like Grafana', () => {
  expect(parseTime('now-6h', NOW, 'utc')).toBe(NOW - 6 * 3_600_000);
  expect(parseTime('now/d', NOW, 'utc')).toBe(Date.UTC(2026, 9, 27));
  expect(parseTime('now/d', NOW, 'utc', true)).toBe(Date.UTC(2026, 9, 28) - 1);
  expect(parseTime('now-1d/d', NOW, 'utc')).toBe(Date.UTC(2026, 9, 26));
  expect(parseTime('now/w', NOW, 'utc')).toBe(Date.UTC(2026, 9, 26));
  expect(parseTime('now-1M/M', NOW, 'utc')).toBe(Date.UTC(2026, 8, 1));
  expect(parseTime('now/y', NOW, 'utc')).toBe(Date.UTC(2026, 0, 1));
  expect(resolveRange({ from: 'now-7d', to: 'now' }, NOW, 'utc')).toEqual({ start: NOW - 7 * 86_400_000, end: NOW });
});

test('reads absolute input in the chosen time zone and stores it as epoch milliseconds', () => {
  expect(parseTime('2026-10-01 08:30', NOW, 'utc')).toBe(Date.UTC(2026, 9, 1, 8, 30));
  expect(parseTime('2026-10-01', NOW, 'browser')).toBe(new Date(2026, 9, 1).getTime());
  expect(parseTime(String(NOW), NOW, 'browser')).toBe(NOW);
  expect(storedText(' 2026-10-01 08:30:00 ', NOW, 'utc', false)).toBe(String(Date.UTC(2026, 9, 1, 8, 30)));
  expect(storedText('now-1h', NOW, 'utc', false)).toBe('now-1h');
  expect(formatAbsolute(Date.UTC(2026, 0, 2, 3, 4, 5), 'utc')).toBe('2026-01-02 03:04:05');
});

test('shifts by half the span without passing now and zooms out by doubling', () => {
  const range = { start: NOW - 4 * 3_600_000, end: NOW - 2 * 3_600_000 };
  expect(shiftRange(range, -1, NOW)).toEqual({ from: String(NOW - 5 * 3_600_000), to: String(NOW - 3 * 3_600_000) });
  expect(shiftRange(range, 1, NOW)).toEqual({ from: String(NOW - 3 * 3_600_000), to: String(NOW - 3_600_000) });
  expect(shiftRange({ start: NOW - 3_600_000, end: NOW }, 1, NOW)).toEqual({
    from: String(NOW - 3_600_000),
    to: String(NOW),
  });
  expect(zoomOutRange({ from: 'now-6h', to: 'now' }, { start: NOW - 6 * 3_600_000, end: NOW })).toEqual({
    from: 'now-12h',
    to: 'now',
  });
  expect(zoomOutRange({ from: 'a', to: 'b' }, range)).toEqual({
    from: String(NOW - 5 * 3_600_000),
    to: String(NOW - 3_600_000),
  });
});

test('accepts only the offered refresh intervals', () => {
  expect(refreshMs('')).toBeNull();
  expect(refreshMs('5x')).toBeNull();
  expect(refreshMs('30s')).toBe(30_000);
  expect(refreshMs('1h')).toBe(3_600_000);
});

test('treats offsets and epochs outside the Date range as invalid instead of producing NaN', () => {
  for (const from of ['now-999999999999M', 'now-999999999999d', 'now-999999999999y', '9007199254740993'])
    expect(parseTime(from, NOW, 'utc')).toBeNull();
  expect(rangeProblem({ from: 'now-999999999999M', to: 'now' }, NOW, 'utc')).toBe('invalid');
  expect(rangeProblem({ from: '9007199254740993', to: '9007200154740993' }, NOW, 'utc')).toBe('invalid');
  expect(resolveRange({ from: 'now-999999999999d', to: 'now' }, NOW, 'browser')).toBeNull();
});

test('clamps month and year arithmetic to the last day of the target month', () => {
  const march31 = Date.UTC(2026, 2, 31, 10);
  expect(parseTime('now-1M', march31, 'utc')).toBe(Date.UTC(2026, 1, 28, 10));
  expect(parseTime('now-1M/M', march31, 'utc')).toBe(Date.UTC(2026, 1, 1));
  expect(parseTime('now-1M/M', march31, 'utc', true)).toBe(Date.UTC(2026, 2, 1) - 1);
  expect(parseTime('now-6M', march31, 'utc')).toBe(Date.UTC(2025, 8, 30, 10));
  expect(parseTime('now-1M', Date.UTC(2026, 4, 31), 'utc')).toBe(Date.UTC(2026, 3, 30));
  expect(parseTime('now+11M', Date.UTC(2026, 2, 31), 'utc')).toBe(Date.UTC(2027, 1, 28));
  expect(parseTime('now-1y', Date.UTC(2028, 1, 29), 'utc')).toBe(Date.UTC(2027, 1, 28));
  expect(parseTime('now-1M', new Date(2026, 2, 31, 9).getTime(), 'browser')).toBe(new Date(2026, 1, 28, 9).getTime());
});

test('accepts only millisecond epochs and rejects out-of-range clock fields', () => {
  expect(parseTime('1700000000', NOW, 'utc')).toBeNull();
  expect(parseTime('1700000000000', NOW, 'utc')).toBe(1_700_000_000_000);
  expect(parseTime('2026-10-01 08:30:99', NOW, 'utc')).toBeNull();
  expect(parseTime('2026-10-01 08:60', NOW, 'utc')).toBeNull();
  expect(parseTime('2026-10-01 24:00', NOW, 'utc')).toBeNull();
  expect(parseTime('2026-10-01 08:30:59', NOW, 'utc')).toBe(Date.UTC(2026, 9, 1, 8, 30, 59));
});
