import { describe, expect, test } from 'bun:test';
import { optionalNumericValueFromKeys, sessionListCost } from './utils';

describe('managed agent numeric fields', () => {
  test('skips blank strings before using a later numeric field', () => {
    expect(optionalNumericValueFromKeys({ primary: '  ', fallback: '42' }, ['primary', 'fallback'])).toBe(42);
  });

  test('parses structured and numeric session list costs', () => {
    expect(sessionListCost({ list_cost: { amount: '125', currency: 'EUR' } })).toEqual({
      amount: 1.25,
      currency: 'EUR',
    });
    expect(sessionListCost({ list_cost: 0.42 })).toEqual({ amount: 0.42, currency: 'USD' });
  });
});
