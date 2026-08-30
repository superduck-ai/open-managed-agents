import { describe, expect, test } from 'bun:test';
import { optionalNumericValueFromKeys } from './utils';

describe('managed agent numeric fields', () => {
  test('skips blank strings before using a later numeric field', () => {
    expect(optionalNumericValueFromKeys({ primary: '  ', fallback: '42' }, ['primary', 'fallback'])).toBe(42);
  });
});
