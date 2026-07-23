import { afterAll, beforeAll, describe, expect, test } from 'bun:test';
import { createdFilterLabel, formatCreatedRange, formatCreatedRangeDay } from './labels';

// Force a UTC- timezone for this file so the legacy `Date.parse` path — which
// treated `yyyy-MM-dd` as UTC midnight — would visibly drift the formatted day
// backward by one. Bun applies the `TZ` change to subsequent
// `Intl.DateTimeFormat` calls, and the helpers format through `Intl`, so they
// run under the new zone. The zone is restored in `afterAll` so the override
// does not leak into other test files in the same run.
const previousTimezone = process.env.TZ;

beforeAll(() => {
  process.env.TZ = 'America/Los_Angeles';
});

afterAll(() => {
  process.env.TZ = previousTimezone;
});

describe('formatCreatedRangeDay', () => {
  test('formats a local calendar day in English', () => {
    expect(formatCreatedRangeDay(new Date(2026, 6, 20), 'en')).toBe('Jul 20, 2026');
  });

  test('formats a local calendar day in Simplified Chinese', () => {
    expect(formatCreatedRangeDay(new Date(2026, 6, 20), 'zh-CN')).toBe('2026年7月20日');
  });
});

describe('formatCreatedRange', () => {
  test('collapses a same-day range to a single label', () => {
    expect(formatCreatedRange(new Date(2026, 6, 20), new Date(2026, 6, 20), 'en')).toBe('Jul 20, 2026');
  });

  test('joins distinct bounds with an en-dash in English', () => {
    expect(formatCreatedRange(new Date(2026, 6, 1), new Date(2026, 6, 20), 'en')).toBe('Jul 1, 2026 – Jul 20, 2026');
  });

  test('joins distinct bounds in Simplified Chinese', () => {
    expect(formatCreatedRange(new Date(2026, 6, 1), new Date(2026, 6, 20), 'zh-CN')).toBe(
      '2026年7月1日 – 2026年7月20日',
    );
  });
});

describe('createdFilterLabel (custom range)', () => {
  test('keeps the selected calendar day stable in a UTC- timezone', () => {
    // Regression for #111: `Date.parse('2026-07-20')` returns UTC midnight,
    // which is July 19 in America/Los_Angeles. `parseISO` parses the value as
    // a local calendar day so the formatted label stays on the 20th.
    expect(createdFilterLabel({ kind: 'custom', from: '2026-07-20', to: '2026-07-20' }, undefined, 'en')).toBe(
      'Jul 20, 2026',
    );
  });

  test('formats a multi-day range in Simplified Chinese', () => {
    expect(createdFilterLabel({ kind: 'custom', from: '2026-07-01', to: '2026-07-20' }, undefined, 'zh-CN')).toBe(
      '2026年7月1日 – 2026年7月20日',
    );
  });

  test('falls back to the localized "Custom range" label for invalid dates', () => {
    expect(createdFilterLabel({ kind: 'custom', from: 'not-a-date', to: '2026-07-20' }, undefined, 'en')).toBe(
      'Custom range',
    );
  });
});

describe('createdFilterLabel (presets)', () => {
  test('renders localized preset labels without a message helper', () => {
    expect(createdFilterLabel({ kind: 'all' })).toBe('All time');
    expect(createdFilterLabel({ kind: 'last7' })).toBe('Last 7 days');
    expect(createdFilterLabel({ kind: 'last30' })).toBe('Last 30 days');
  });
});
