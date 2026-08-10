import { expect, test } from 'bun:test';
import { relativeTime } from './model';

test('keeps localized relative-time units on the completed interval', () => {
  const timestamp = new Date(Date.now() - 170_000).toISOString();
  expect(relativeTime(timestamp, (value, unit) => `${value}:${unit}`)).toBe('-2:minute');
});
